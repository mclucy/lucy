package resolve_test

import (
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/resolve"
	serverpkg "github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
)

const (
	rootAbsent = iota
	rootAny
	rootStable
	rootExactVersion
	rootMissingVersion
	rootDisabled
	rootReleaseID
	rootFilePattern
)

type selectionCandidateScenario struct {
	version       string
	channel       string
	releaseID     string
	fileID        string
	filename      string
	primary       bool
	requiresGhost bool
	deps          [3]int
}

type selectionProjectScenario struct {
	rootMode   int
	candidates []selectionCandidateScenario
}

type selectionScenario struct {
	expand   bool
	projects [3]selectionProjectScenario
}

type selectionAssignment [3]int

// FuzzPackagesProjectSelection drives the public resolver with generated
// requirement, dependency, and incompatibility graphs. The oracle enumerates
// every candidate assignment and checks manifest satisfaction, required
// dependency closure, incompatibility, primary-file rules, reachability, and
// the dependency provenance the lock must record.
func FuzzPackagesProjectSelection(f *testing.F) {
	// A required dependency overrides a disabled direct binding: beta is
	// declared enabled:false but alpha requires it, so the selected beta must
	// still be enabled for installation. This seed reproduces the returned
	// package whose only requirement binding is disabled.
	f.Add(
		true,
		[]byte{rootAny, 1, 0, depKindRequired, depKindNone},
		[]byte{rootDisabled, 1, 0, depKindNone, depKindNone},
		[]byte{rootAbsent, 1, 0, depKindNone, depKindNone},
	)
	// A root any requirement expands one required dependency.
	f.Add(
		true,
		[]byte{rootAny, 1, 0, depKindRequired, depKindNone},
		[]byte{rootAbsent, 1, 0, depKindNone, depKindNone},
		[]byte{rootDisabled, 1, 0, depKindNone, depKindNone},
	)
	// The newest candidate needs a project whose only candidate cannot answer
	// a dependency, so resolution must backtrack to the older candidate.
	f.Add(
		true,
		[]byte{rootAny, 2, 0, depKindNone, depKindRequired, 0, depKindNone, depKindNone},
		[]byte{rootAny, 1, 0, depKindNone, depKindNone},
		[]byte{rootAbsent, 1, 2, depKindNone, depKindNone},
	)
	// A required dependency that resolves to no acceptable build.
	f.Add(
		true,
		[]byte{rootAny, 1, 4, depKindNone, depKindNone},
		[]byte{rootAbsent, 1, 0, depKindNone, depKindNone},
		[]byte{rootAbsent, 1, 0, depKindNone, depKindNone},
	)
	// Every root selects without expanding catalogue dependencies.
	f.Add(
		false,
		[]byte{rootAny, 1, 0, depKindNone, depKindNone},
		[]byte{rootAny, 1, 0, depKindNone, depKindNone},
		[]byte{rootAny, 1, 0, depKindNone, depKindNone},
	)
	// Stable rejects the prerelease candidate and keeps the release fallback.
	f.Add(
		false,
		[]byte{rootStable, 2, 1, depKindNone, depKindNone, 0, depKindNone, depKindNone},
		[]byte{rootAbsent, 1, 0, depKindNone, depKindNone},
		[]byte{rootAbsent, 1, 0, depKindNone, depKindNone},
	)
	// Disabled bindings alone select nothing.
	f.Add(
		true,
		[]byte{rootDisabled, 1, 0, depKindNone, depKindNone},
		[]byte{rootDisabled, 1, 0, depKindNone, depKindNone},
		[]byte{rootDisabled, 1, 0, depKindNone, depKindNone},
	)
	// A file-pattern requirement accepts a matching non-primary file.
	f.Add(
		false,
		[]byte{rootFilePattern, 1, 2, depKindNone, depKindNone},
		[]byte{rootAbsent, 1, 0, depKindNone, depKindNone},
		[]byte{rootAbsent, 1, 0, depKindNone, depKindNone},
	)
	// An exact release identity selects the candidate without version syntax.
	f.Add(
		false,
		[]byte{rootReleaseID, 1, 0, depKindNone, depKindNone},
		[]byte{rootAbsent, 1, 0, depKindNone, depKindNone},
		[]byte{rootAbsent, 1, 0, depKindNone, depKindNone},
	)

	f.Fuzz(func(t *testing.T, expand bool, p0, p1, p2 []byte) {
		scenario := decodeSelectionScenario(expand, [3][]byte{p0, p1, p2})
		fixture := selectionFixture(t, scenario)
		fixture.install(t)

		out, warnings, err := resolve.Packages(resolve.PackagesRequest{
			Context:            t.Context(),
			Manifest:           selectionManifest(scenario),
			Target:             fuzzTarget,
			Materialize:        fixture.materialize,
			ExpandDependencies: scenario.expand,
		})
		if err != nil {
			if selectionHasSolution(scenario) {
				t.Fatalf(
					"Packages() error = %v, but the graph is satisfiable; scenario: %#v",
					err,
					scenario,
				)
			}
			if out != nil {
				t.Fatalf("Packages() returned %d packages with an error", len(out))
			}
			if len(warnings) != 0 {
				t.Fatalf("Packages() returned %d warnings with an error", len(warnings))
			}
			return
		}

		if !selectionHasSolution(scenario) {
			t.Fatalf("Packages() succeeded although no assignment satisfies the graph: %#v", scenario)
		}
		assignment, err := selectionAssignmentFromResult(scenario, out)
		if err != nil {
			t.Fatalf("Packages() result is not a valid selection: %v; scenario: %#v", err, scenario)
		}
		if err := selectionAssignmentValid(scenario, assignment); err != nil {
			t.Fatalf("Packages() selected an invalid assignment: %v; scenario: %#v", err, scenario)
		}
		if len(warnings) != 0 {
			t.Fatalf("Packages() returned %d warnings for a valid graph", len(warnings))
		}
	})
}

func decodeSelectionScenario(expand bool, raw [3][]byte) selectionScenario {
	scenario := selectionScenario{expand: expand}
	for p := range fuzzProjectNames {
		data := raw[p]
		position := 0
		next := func() int {
			if position >= len(data) {
				return 0
			}
			value := int(data[position])
			position++
			return value
		}
		project := &scenario.projects[p]
		project.rootMode = next() % 8
		count := 1 + next()%2
		for candidateIndex := range count {
			flags := next()
			candidate := selectionCandidateScenario{
				version:       selectionCandidateVersion(candidateIndex),
				channel:       "release",
				releaseID:     selectionReleaseID(fuzzProjectNames[p], candidateIndex),
				fileID:        selectionFileID(fuzzProjectNames[p], candidateIndex),
				filename:      selectionFilename(fuzzProjectNames[p], candidateIndex),
				primary:       flags&2 == 0,
				requiresGhost: flags&4 != 0,
			}
			if flags&1 != 0 {
				candidate.channel = "beta"
			}
			for q := range fuzzProjectNames {
				if q == p {
					continue
				}
				kind := next() % 4
				if kind > depKindIncompatible {
					kind = depKindNone
				}
				candidate.deps[q] = kind
			}
			project.candidates = append(project.candidates, candidate)
		}
	}
	return scenario
}

func selectionFixture(t *testing.T, scenario selectionScenario) *catalogueFixture {
	t.Helper()
	fixture := newCatalogueFixture(t)
	for p, name := range fuzzProjectNames {
		versions := make([]stubVersion, 0, len(scenario.projects[p].candidates))
		for candidateIndex, candidate := range scenario.projects[p].candidates {
			dependencies := make([]stubDependency, 0, 3)
			for q := range fuzzProjectNames {
				if q == p {
					continue
				}
				switch candidate.deps[q] {
				case depKindRequired:
					dependencies = append(dependencies, stubDependency{
						ProjectID:      fuzzProjectNames[q],
						DependencyType: "required",
					})
				case depKindIncompatible:
					dependencies = append(dependencies, stubDependency{
						ProjectID:      fuzzProjectNames[q],
						DependencyType: "incompatible",
					})
				}
			}
			if candidate.requiresGhost {
				dependencies = append(dependencies, stubDependency{
					ProjectID:      "ghost",
					DependencyType: "required",
				})
			}
			version := stubVersionEntry(name, candidateIndex, candidate.version, candidate.channel, candidate.primary)
			version.Dependencies = dependencies
			versions = append(versions, version)
			fixture.addArchive(t, candidate.fileID, fabricArchive(t, fabricModuleSpec{
				ID:      name,
				Version: candidate.version,
			}, nil))
		}
		fixture.addVersions(t, name, versions)
	}
	// A published build exists for the ghost project, but it is not primary,
	// so a dependency request can find it and still reject it.
	fixture.addVersions(t, "ghost", []stubVersion{
		stubVersionEntry("ghost", 0, selectionCandidateVersion(0), "release", false),
	})
	return fixture
}

func selectionManifest(scenario selectionScenario) *manifest.Document {
	packages := make(map[string]manifest.Requirement)
	for p, name := range fuzzProjectNames {
		if requirement, ok := selectionRootRequirement(name, scenario.projects[p].rootMode); ok {
			packages["modrinth:"+name] = requirement
		}
	}
	return &manifest.Document{
		Server: manifest.Server{
			Minecraft: versionpkg.Version(fuzzMinecraft),
			Packages: map[catalog.Ecosystem]map[string]manifest.Requirement{
				catalog.EcosystemFabric: packages,
			},
		},
	}
}

func selectionRootRequirement(name string, mode int) (manifest.Requirement, bool) {
	disabled := false
	switch mode {
	case rootAbsent:
		return manifest.Requirement{}, false
	case rootAny:
		return manifest.Requirement{Selector: versionpkg.Any}, true
	case rootStable:
		return manifest.Requirement{Selector: versionpkg.Stable}, true
	case rootExactVersion:
		return manifest.Requirement{Selector: versionpkg.Version("1.0.0")}, true
	case rootMissingVersion:
		return manifest.Requirement{Selector: versionpkg.Version("9.9.9")}, true
	case rootDisabled:
		return manifest.Requirement{Selector: versionpkg.Any, Enabled: &disabled}, true
	case rootReleaseID:
		return manifest.Requirement{Selector: versionpkg.Version(selectionReleaseID(name, 0))}, true
	case rootFilePattern:
		return manifest.Requirement{Selector: versionpkg.Any, FilePattern: "*-0.jar"}, true
	default:
		return manifest.Requirement{}, false
	}
}

func selectionRootEnabled(mode int) bool {
	switch mode {
	case rootAny, rootStable, rootExactVersion, rootMissingVersion, rootReleaseID, rootFilePattern:
		return true
	default:
		return false
	}
}

func selectionRootAccepts(mode int, project string, candidate selectionCandidateScenario) bool {
	switch mode {
	case rootAny:
		return candidate.primary
	case rootStable:
		return candidate.primary && candidate.channel == "release"
	case rootExactVersion:
		return candidate.primary && selectionCandidateMatches(candidate, "1.0.0")
	case rootMissingVersion:
		return candidate.primary && selectionCandidateMatches(candidate, "9.9.9")
	case rootReleaseID:
		return candidate.primary && selectionCandidateMatches(candidate, selectionReleaseID(project, 0))
	case rootFilePattern:
		// decideCandidate skips the primary check when a file pattern decides
		// eligibility.
		matched, err := filepath.Match("*-0.jar", candidate.filename)
		return err == nil && matched
	default:
		return false
	}
}

func selectionCandidateMatches(candidate selectionCandidateScenario, selector string) bool {
	return candidate.version == selector ||
		candidate.releaseID == selector ||
		candidate.fileID == selector ||
		candidate.filename == selector
}

func selectionAssignmentValid(scenario selectionScenario, chosen selectionAssignment) error {
	for p, name := range fuzzProjectNames {
		mode := scenario.projects[p].rootMode
		root := selectionRootEnabled(mode)
		if chosen[p] < 0 {
			if root {
				return fmt.Errorf("enabled requirement for %s selected no package", name)
			}
			continue
		}
		if chosen[p] >= len(scenario.projects[p].candidates) {
			return fmt.Errorf("selected %s candidate %d outside the generated list", name, chosen[p])
		}
		candidate := scenario.projects[p].candidates[chosen[p]]
		reachable := root
		if scenario.expand {
			for q := range fuzzProjectNames {
				if q == p || chosen[q] < 0 {
					continue
				}
				if scenario.projects[q].candidates[chosen[q]].deps[p] == depKindRequired {
					reachable = true
					break
				}
			}
		}
		if !reachable {
			return fmt.Errorf("%s was selected without a manifest requirement or required dependency", name)
		}
		if root {
			if !selectionRootAccepts(mode, name, candidate) {
				return fmt.Errorf("candidate %s of %s does not satisfy root mode %d", candidate.version, name, mode)
			}
		} else if !candidate.primary {
			// Dependency requests carry no requirement and reject non-primary
			// files; a disabled binding does not relax that.
			return fmt.Errorf("%s was selected for a dependency from a non-primary file", name)
		}
		if !scenario.expand {
			for q := range fuzzProjectNames {
				if q != p && candidate.deps[q] == depKindIncompatible && chosen[q] >= 0 {
					return fmt.Errorf("%s conflicts with selected %s", name, fuzzProjectNames[q])
				}
			}
			continue
		}
		if candidate.requiresGhost {
			return fmt.Errorf("candidate %s of %s requires a project with no acceptable build", candidate.version, name)
		}
		for q := range fuzzProjectNames {
			if q == p {
				continue
			}
			switch candidate.deps[q] {
			case depKindRequired:
				if chosen[q] < 0 {
					return fmt.Errorf("%s requires %s but it is not selected", name, fuzzProjectNames[q])
				}
			case depKindIncompatible:
				if chosen[q] >= 0 {
					return fmt.Errorf("%s conflicts with selected %s", name, fuzzProjectNames[q])
				}
			}
		}
	}
	return nil
}

func selectionHasSolution(scenario selectionScenario) bool {
	var chosen selectionAssignment
	var search func(project int) bool
	search = func(project int) bool {
		if project == len(fuzzProjectNames) {
			return selectionAssignmentValid(scenario, chosen) == nil
		}
		chosen[project] = -1
		if search(project + 1) {
			return true
		}
		for candidate := range scenario.projects[project].candidates {
			chosen[project] = candidate
			if search(project + 1) {
				return true
			}
		}
		chosen[project] = -1
		return false
	}
	return search(0)
}

func selectionAssignmentFromResult(
	scenario selectionScenario,
	out []lockfile.Package,
) (selectionAssignment, error) {
	chosen := selectionAssignment{-1, -1, -1}
	for _, pkg := range out {
		project := slices.Index(fuzzProjectNames[:], pkg.ProjectID)
		if project < 0 {
			return chosen, fmt.Errorf("selected unknown project %q", pkg.ProjectID)
		}
		if chosen[project] >= 0 {
			return chosen, fmt.Errorf("selected project %q more than once", pkg.ProjectID)
		}
		if pkg.Environment != serverpkg.EnvironmentServer ||
			pkg.Ecosystem != catalog.EcosystemFabric ||
			pkg.Provider != catalog.ProviderModrinth {
			return chosen, fmt.Errorf(
				"selected %s as %s/%s via %s",
				pkg.ProjectID,
				pkg.Environment,
				pkg.Ecosystem,
				pkg.Provider,
			)
		}
		candidateIndex := -1
		for index, candidate := range scenario.projects[project].candidates {
			if pkg.Artifact.Version == versionpkg.Version(candidate.version) &&
				pkg.ReleaseID == candidate.releaseID &&
				pkg.FileID == candidate.fileID &&
				pkg.Filename == candidate.filename {
				candidateIndex = index
				break
			}
		}
		if candidateIndex < 0 {
			return chosen, fmt.Errorf(
				"selected %s %s (%s/%s/%s) does not match a generated candidate",
				pkg.ProjectID,
				pkg.Version,
				pkg.ReleaseID,
				pkg.FileID,
				pkg.Filename,
			)
		}
		candidate := scenario.projects[project].candidates[candidateIndex]
		if len(pkg.Modules) != 1 ||
			pkg.Modules[0].ID != fuzzProjectNames[project] ||
			pkg.Modules[0].Version != versionpkg.Version(candidate.version) ||
			pkg.Modules[0].NestedPath != "" {
			return chosen, fmt.Errorf(
				"selected %s modules = %#v, want one native module %s@%s",
				pkg.ProjectID,
				pkg.Modules,
				fuzzProjectNames[project],
				candidate.version,
			)
		}
		if err := selectionRequirementMatches(scenario, project, pkg); err != nil {
			return chosen, err
		}
		if !pkg.Enabled() {
			return chosen, fmt.Errorf(
				"selected %s is not enabled for installation; requirements = %#v",
				pkg.ProjectID,
				pkg.Requirements,
			)
		}
		chosen[project] = candidateIndex
	}
	if !slices.IsSortedFunc(out, comparePackageOrder) {
		return chosen, fmt.Errorf("selected packages are not sorted by identity")
	}
	if err := selectionProvenance(scenario, chosen, out); err != nil {
		return chosen, err
	}
	return chosen, nil
}

// selectionProvenance checks the persisted dependency provenance against the
// generated graph: the required dependents of every selected package and
// whether another package selected it.
func selectionProvenance(
	scenario selectionScenario,
	chosen selectionAssignment,
	out []lockfile.Package,
) error {
	byProject := make(map[int]*lockfile.Package, len(out))
	for index := range out {
		project := slices.Index(fuzzProjectNames[:], out[index].ProjectID)
		byProject[project] = &out[index]
	}
	for project := range fuzzProjectNames {
		pkg, selected := byProject[project]
		if !selected {
			continue
		}
		var wantSources []catalog.Ref
		if scenario.expand {
			for parent := range fuzzProjectNames {
				if parent == project || chosen[parent] < 0 {
					continue
				}
				if scenario.projects[parent].candidates[chosen[parent]].deps[project] != depKindRequired {
					continue
				}
				wantSources = append(wantSources, catalog.Ref{
					Name:     catalog.Name(fuzzProjectNames[parent]),
					Provider: catalog.ProviderModrinth,
				})
			}
		}
		if !sameRefSet(pkg.DependencyOf, wantSources) {
			return fmt.Errorf(
				"selected %s DependencyOf = %#v, want %#v",
				pkg.ProjectID,
				pkg.DependencyOf,
				wantSources,
			)
		}
		if wantDependency := len(wantSources) > 0; pkg.Dependency != wantDependency {
			return fmt.Errorf(
				"selected %s Dependency = %v, want %v; dependents = %#v",
				pkg.ProjectID,
				pkg.Dependency,
				wantDependency,
				wantSources,
			)
		}
		if scenario.projects[project].rootMode == rootAbsent && len(pkg.Requirements) != 0 {
			return fmt.Errorf(
				"selected %s has requirement bindings without a declaration: %#v",
				pkg.ProjectID,
				pkg.Requirements,
			)
		}
	}
	return nil
}

func sameRefSet(actual, want []catalog.Ref) bool {
	if len(actual) != len(want) {
		return false
	}
	actualSet := make(map[catalog.Ref]int, len(actual))
	for _, ref := range actual {
		actualSet[ref]++
	}
	wantSet := make(map[catalog.Ref]int, len(want))
	for _, ref := range want {
		wantSet[ref]++
	}
	return maps.Equal(actualSet, wantSet)
}

func selectionRequirementMatches(scenario selectionScenario, project int, pkg lockfile.Package) error {
	mode := scenario.projects[project].rootMode
	name := fuzzProjectNames[project]
	if mode == rootDisabled {
		// The disabled direct declaration stays on the package alongside the
		// dependency demand that selected it. Enabled() below is the
		// installation contract; do not pin any additional demand bindings.
		direct := lockfile.RequirementBinding{
			Reference: "modrinth:" + name,
			Selector:  versionpkg.Any,
			Enabled:   false,
		}
		if !slices.Contains(pkg.Requirements, direct) {
			return fmt.Errorf(
				"selected %s lost its disabled direct binding; requirements = %#v",
				name,
				pkg.Requirements,
			)
		}
		return nil
	}
	if !selectionRootEnabled(mode) {
		return nil
	}
	requirement, _ := selectionRootRequirement(name, mode)
	direct := lockfile.RequirementBinding{
		Reference:   "modrinth:" + name,
		Selector:    requirement.Selector,
		FilePattern: requirement.FilePattern,
		Enabled:     requirement.IsEnabled(),
	}
	if !slices.Contains(pkg.Requirements, direct) {
		return fmt.Errorf(
			"selected %s lost its direct binding %#v; requirements = %#v",
			name,
			direct,
			pkg.Requirements,
		)
	}
	return nil
}

func comparePackageOrder(a, b lockfile.Package) int {
	if order := strings.Compare(string(a.Environment), string(b.Environment)); order != 0 {
		return order
	}
	if order := strings.Compare(string(a.Ecosystem), string(b.Ecosystem)); order != 0 {
		return order
	}
	if order := strings.Compare(string(a.Provider), string(b.Provider)); order != 0 {
		return order
	}
	return strings.Compare(a.ProjectID, b.ProjectID)
}
