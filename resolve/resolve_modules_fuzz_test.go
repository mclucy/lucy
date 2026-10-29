package resolve_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/resolve"
	versionpkg "github.com/mclucy/lucy/version"
)

const moduleDependencyCount = 10

var (
	moduleIdentities    = [5]string{"alpha", "beta", "gamma", "shared", "extras"}
	moduleDependencyIDs = [moduleDependencyCount]string{
		"alpha", "beta", "gamma", "shared", "extras",
		"lucycore", "minecraft", "fabricloader", "java", "ghostmod",
	}
	moduleRuntimeIDs = map[string]string{
		"minecraft":    fuzzMinecraft,
		"fabricloader": fuzzLoaderVersion,
		"lucycore":     "3.0.0",
	}
)

type moduleProjectScenario struct {
	outerID       int
	outerVersion  int
	provides      int
	nested        bool
	nestedID      int
	nestedVersion int
	deps          [moduleDependencyCount]int
}

type moduleScenario struct {
	acceptBroken bool
	projects     [3]moduleProjectScenario
}

type moduleDependency struct {
	id           string
	incompatible bool
}

type moduleDeclaration struct {
	project    int
	id         string
	version    string
	nestedPath string
	provides   []string
	deps       []moduleDependency
}

type moduleKey struct {
	id      string
	version string
	nested  string
}

type moduleProblemExpectation struct {
	owner      string
	dependency string
}

// FuzzPackagesModuleGraph drives the public resolver with generated descriptor
// module graphs packaged into real Fabric archives. The oracle applies the
// documented ownership rules (native outranks embedded, embedded compares
// versions, two native claims in different packages are ambiguous) and checks
// every required or incompatible module dependency independently.
func FuzzPackagesModuleGraph(f *testing.F) {
	// A required project dependency selects beta even though its direct
	// manifest binding is disabled. Beta's module constraints must still be
	// checked: its missing required module is a resolution failure. A package
	// treated as disabled here skips finalizeModules and resolves silently.
	f.Add(
		false,
		true,
		moduleSeed(0, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
		moduleSeed(1, 0, 0, false, 0, 0, [moduleDependencyCount]int{9: depKindRequired}),
		moduleSeed(2, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
	)
	// The same demand-selected package must report Enabled for installation
	// and must preserve its disabled direct binding.
	f.Add(
		false,
		true,
		moduleSeed(0, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
		moduleSeed(1, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
		moduleSeed(2, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
	)
	// AcceptBroken reports the demand-selected package's module problems as
	// warnings instead of failing.
	f.Add(
		true,
		true,
		moduleSeed(0, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
		moduleSeed(1, 0, 0, false, 0, 0, [moduleDependencyCount]int{9: depKindRequired}),
		moduleSeed(2, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
	)
	// Three distinct native modules.
	f.Add(
		false,
		false,
		moduleSeed(0, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
		moduleSeed(1, 1, 0, false, 0, 0, [moduleDependencyCount]int{}),
		moduleSeed(2, 2, 0, false, 0, 0, [moduleDependencyCount]int{}),
	)
	// Two embedded copies of one identity; the higher version owns it.
	f.Add(
		false,
		false,
		moduleSeed(0, 0, 0, true, 3, 0, [moduleDependencyCount]int{}),
		moduleSeed(1, 0, 0, true, 3, 1, [moduleDependencyCount]int{}),
		moduleSeed(2, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
	)
	// Two native declarations of one identity are ambiguous.
	f.Add(
		false,
		false,
		moduleSeed(0, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
		moduleSeed(0, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
		moduleSeed(1, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
	)
	// A required module dependency no package provides.
	f.Add(
		false,
		false,
		moduleSeed(0, 0, 0, false, 0, 0, [moduleDependencyCount]int{9: depKindRequired}),
		moduleSeed(1, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
		moduleSeed(2, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
	)
	// AcceptBroken turns the same module problems into warnings.
	f.Add(
		true,
		false,
		moduleSeed(0, 0, 0, false, 0, 0, [moduleDependencyCount]int{9: depKindRequired}),
		moduleSeed(1, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
		moduleSeed(2, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
	)
	// An incompatible module dependency on a selected package.
	f.Add(
		false,
		false,
		moduleSeed(0, 0, 0, false, 0, 0, [moduleDependencyCount]int{1: depKindIncompatible}),
		moduleSeed(1, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
		moduleSeed(2, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
	)
	// Runtime-provided modules satisfy requirements.
	f.Add(
		false,
		false,
		moduleSeed(0, 0, 0, false, 0, 0, [moduleDependencyCount]int{
			5: depKindRequired,
			6: depKindRequired,
			7: depKindRequired,
		}),
		moduleSeed(1, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
		moduleSeed(2, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
	)
	// A provided alias answers another module's dependency.
	f.Add(
		false,
		false,
		moduleSeed(0, 0, 4, false, 0, 0, [moduleDependencyCount]int{}),
		moduleSeed(1, 0, 0, false, 0, 0, [moduleDependencyCount]int{3: depKindRequired}),
		moduleSeed(2, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
	)
	// A native module outranks an embedded module of the same identity.
	f.Add(
		false,
		false,
		moduleSeed(0, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
		moduleSeed(1, 0, 0, true, 0, 2, [moduleDependencyCount]int{}),
		moduleSeed(2, 0, 0, false, 0, 0, [moduleDependencyCount]int{}),
	)

	f.Fuzz(func(t *testing.T, acceptBroken, demand bool, p0, p1, p2 []byte) {
		scenario := decodeModuleScenario(acceptBroken, [3][]byte{p0, p1, p2})
		declarations := moduleDeclarations(scenario)
		claims := moduleClaims(declarations)
		duplicateIdentity, duplicate := moduleDuplicateNative(declarations, claims)
		fixture := moduleFixture(t, scenario, demand)

		out, warnings, err := resolve.Packages(resolve.PackagesRequest{
			Context:            t.Context(),
			Manifest:           moduleManifest(demand),
			Target:             fuzzModuleTarget,
			Candidates:         fixture.candidates,
			Materialize:        fixture.materialize,
			AcceptBroken:       scenario.acceptBroken,
			ExpandDependencies: demand,
		})
		if duplicate {
			if err == nil {
				t.Fatalf("Packages() accepted the ambiguous native module %q; scenario: %#v", duplicateIdentity, scenario)
			}
			if !strings.Contains(err.Error(), "duplicate native module") {
				t.Fatalf("Packages() error = %v, want a duplicate native module report", err)
			}
			if out != nil || len(warnings) != 0 {
				t.Fatalf("Packages() returned output with an ambiguous native module")
			}
			return
		}

		problems, oracleErr := moduleExpectedProblems(declarations, claims)
		if oracleErr != nil {
			t.Fatalf("module oracle: %v", oracleErr)
		}
		switch {
		case len(problems) == 0:
			if err != nil {
				t.Fatalf("Packages() error = %v, want a valid module graph", err)
			}
			if len(warnings) != 0 {
				t.Fatalf("Packages() returned %d warnings for a valid module graph", len(warnings))
			}
		case !scenario.acceptBroken:
			if err == nil {
				t.Fatalf("Packages() accepted %d unsatisfied module requirements; scenario: %#v", len(problems), scenario)
			}
			if out != nil || len(warnings) != 0 {
				t.Fatalf("Packages() returned output with module problems")
			}
			if matchErr := matchModuleProblems(problems, collectModuleProblems(err)); matchErr != nil {
				t.Fatalf("Packages() error = %v: %v; scenario: %#v", err, matchErr, scenario)
			}
			return
		default:
			if err != nil {
				t.Fatalf("Packages() error = %v with AcceptBroken; scenario: %#v", err, scenario)
			}
			if len(warnings) != len(problems) {
				t.Fatalf(
					"Packages() warnings = %d, want %d; scenario: %#v",
					len(warnings),
					len(problems),
					scenario,
				)
			}
			reported := make([]resolve.ModuleProblem, 0, len(warnings))
			for _, warning := range warnings {
				problem, ok := warning.(resolve.ModuleProblem)
				if !ok {
					t.Fatalf("Packages() warning = %T, want resolve.ModuleProblem", warning)
				}
				reported = append(reported, problem)
			}
			if matchErr := matchModuleProblems(problems, reported); matchErr != nil {
				t.Fatalf("Packages() warnings: %v; scenario: %#v", matchErr, scenario)
			}
		}

		expected, oracleErr := moduleExpectedOutput(declarations, claims)
		if oracleErr != nil {
			t.Fatalf("module oracle: %v", oracleErr)
		}
		if err := validateModuleOutput(out, expected); err != nil {
			t.Fatalf("Packages() modules: %v; scenario: %#v", err, scenario)
		}
		if err := validateModuleBindings(demand, out); err != nil {
			t.Fatalf("Packages() requirements: %v; scenario: %#v", err, scenario)
		}
	})
}

func moduleSeed(
	outerID int,
	outerVersion int,
	provides int,
	nested bool,
	nestedID int,
	nestedVersion int,
	deps [moduleDependencyCount]int,
) []byte {
	nestedFlag := 0
	if nested {
		nestedFlag = 1
	}
	out := []byte{
		byte(outerID),
		byte(outerVersion),
		byte(provides),
		byte(nestedFlag),
		byte(nestedID),
		byte(nestedVersion),
	}
	for _, kind := range deps {
		out = append(out, byte(kind))
	}
	return out
}

func decodeModuleScenario(acceptBroken bool, raw [3][]byte) moduleScenario {
	scenario := moduleScenario{acceptBroken: acceptBroken}
	for p := range fuzzProjectNames {
		data := raw[p]
		at := func(position int) int {
			if position >= len(data) {
				return 0
			}
			return int(data[position])
		}
		project := moduleProjectScenario{
			outerID:       at(0) % len(moduleIdentities),
			outerVersion:  at(1) % 3,
			provides:      -1,
			nested:        at(3)%2 == 1,
			nestedID:      at(4) % len(moduleIdentities),
			nestedVersion: at(5) % 3,
		}
		if rawProvides := at(2); rawProvides != 0 {
			project.provides = (rawProvides - 1) % len(moduleIdentities)
		}
		for index := range moduleDependencyCount {
			project.deps[index] = at(6+index) % 3
		}
		scenario.projects[p] = project
	}
	return scenario
}

func moduleVersion(selection, project int) string {
	return fmt.Sprintf("%d.%d.0", selection+1, project)
}

func moduleFixture(t *testing.T, scenario moduleScenario, demand bool) *catalogueFixture {
	t.Helper()
	fixture := newCatalogueFixture(t)
	for p, name := range fuzzProjectNames {
		project := scenario.projects[p]
		outer := fabricModuleSpec{
			ID:      moduleIdentities[project.outerID],
			Version: moduleVersion(project.outerVersion, p),
		}
		if project.provides >= 0 {
			outer.Provides = []string{moduleIdentities[project.provides]}
		}
		for index, kind := range project.deps {
			switch kind {
			case depKindRequired:
				outer.Depends = append(outer.Depends, moduleDependencyIDs[index])
			case depKindIncompatible:
				outer.Breaks = append(outer.Breaks, moduleDependencyIDs[index])
			}
		}
		var nested *fabricModuleSpec
		if project.nested {
			nested = &fabricModuleSpec{
				ID:      moduleIdentities[project.nestedID],
				Version: moduleVersion(project.nestedVersion, p),
			}
		}
		fixture.addArchive(t, selectionFileID(name, 0), fabricArchive(t, outer, nested))
		version := stubVersionEntry(name, 0, selectionCandidateVersion(0), "release", true)
		if demand && p == 0 {
			version.Dependencies = []stubDependency{{
				ProjectID:      fuzzProjectNames[1],
				DependencyType: "required",
			}}
		}
		fixture.addVersions(t, name, []stubVersion{version})
	}
	return fixture
}

func moduleManifest(demand bool) *document.Manifest {
	disabled := false
	packages := make(map[string]document.Requirement, len(fuzzProjectNames))
	for p, name := range fuzzProjectNames {
		requirement := document.Requirement{Selector: versionpkg.Any}
		if demand && p == 1 {
			requirement.Enabled = &disabled
		}
		packages["modrinth:"+name] = requirement
	}
	return &document.Manifest{
		Server: document.ManifestServer{
			Minecraft: versionpkg.Version(fuzzMinecraft),
			Packages: map[catalog.Ecosystem]map[string]document.Requirement{
				catalog.EcosystemFabric: packages,
			},
		},
	}
}

func moduleDeclarations(scenario moduleScenario) []moduleDeclaration {
	declarations := make([]moduleDeclaration, 0, 2*len(fuzzProjectNames))
	for p := range fuzzProjectNames {
		project := scenario.projects[p]
		outer := moduleDeclaration{
			project: p,
			id:      moduleIdentities[project.outerID],
			version: moduleVersion(project.outerVersion, p),
		}
		if project.provides >= 0 {
			outer.provides = []string{moduleIdentities[project.provides]}
		}
		for index, kind := range project.deps {
			if kind == depKindNone {
				continue
			}
			outer.deps = append(outer.deps, moduleDependency{
				id:           moduleDependencyIDs[index],
				incompatible: kind == depKindIncompatible,
			})
		}
		declarations = append(declarations, outer)
		if project.nested {
			declarations = append(declarations, moduleDeclaration{
				project:    p,
				id:         moduleIdentities[project.nestedID],
				version:    moduleVersion(project.nestedVersion, p),
				nestedPath: nestedJarPath,
			})
		}
	}
	return declarations
}

func moduleClaims(declarations []moduleDeclaration) map[string][]int {
	claims := make(map[string][]int, len(declarations))
	for index, declaration := range declarations {
		seen := map[string]bool{}
		identities := append([]string{declaration.id}, declaration.provides...)
		for _, identity := range identities {
			if seen[identity] {
				continue
			}
			seen[identity] = true
			claims[identity] = append(claims[identity], index)
		}
	}
	return claims
}

func moduleOwner(
	id string,
	claims map[string][]int,
	declarations []moduleDeclaration,
) (int, bool, error) {
	claimants := claims[id]
	if len(claimants) == 0 {
		return -1, false, nil
	}
	for _, index := range claimants {
		if declarations[index].nestedPath == "" {
			return index, true, nil
		}
	}
	best := claimants[0]
	for _, index := range claimants[1:] {
		order, err := compareModuleVersions(declarations[index].version, declarations[best].version)
		if err != nil {
			return -1, false, err
		}
		if order > 0 {
			best = index
		}
	}
	return best, true, nil
}

func compareModuleVersions(left, right string) (int, error) {
	leftVersion, err := versionpkg.ParseVersion(versionpkg.Version(left), versionpkg.DialectFabric)
	if err != nil {
		return 0, fmt.Errorf("parse module version %q: %w", left, err)
	}
	rightVersion, err := versionpkg.ParseVersion(versionpkg.Version(right), versionpkg.DialectFabric)
	if err != nil {
		return 0, fmt.Errorf("parse module version %q: %w", right, err)
	}
	order, comparable := versionpkg.Compare(leftVersion, rightVersion)
	if !comparable {
		return 0, fmt.Errorf("module versions %q and %q are incomparable", left, right)
	}
	return order, nil
}

func moduleDuplicateNative(
	declarations []moduleDeclaration,
	claims map[string][]int,
) (string, bool) {
	identities := make([]string, 0, len(claims))
	for identity := range claims {
		identities = append(identities, identity)
	}
	slices.Sort(identities)
	for _, identity := range identities {
		nativeProjects := map[int]struct{}{}
		for _, index := range claims[identity] {
			if declarations[index].nestedPath == "" {
				nativeProjects[declarations[index].project] = struct{}{}
			}
		}
		if len(nativeProjects) > 1 {
			return identity, true
		}
	}
	return "", false
}

func moduleExpectedOutput(
	declarations []moduleDeclaration,
	claims map[string][]int,
) (map[int][]moduleKey, error) {
	expected := map[int][]moduleKey{}
	for index, declaration := range declarations {
		owner, claimed, err := moduleOwner(declaration.id, claims, declarations)
		if err != nil {
			return nil, err
		}
		if !claimed || owner != index {
			continue
		}
		expected[declaration.project] = append(expected[declaration.project], moduleKey{
			id:      declaration.id,
			version: declaration.version,
			nested:  declaration.nestedPath,
		})
	}
	return expected, nil
}

func moduleExpectedProblems(
	declarations []moduleDeclaration,
	claims map[string][]int,
) ([]moduleProblemExpectation, error) {
	var problems []moduleProblemExpectation
	for index, declaration := range declarations {
		owner, claimed, err := moduleOwner(declaration.id, claims, declarations)
		if err != nil {
			return nil, err
		}
		if !claimed || owner != index {
			continue
		}
		for _, dependency := range declaration.deps {
			if dependency.id == "java" {
				continue
			}
			_, packageProvider, err := moduleOwner(dependency.id, claims, declarations)
			if err != nil {
				return nil, err
			}
			provided := packageProvider || moduleRuntimeIDs[dependency.id] != ""
			switch {
			case dependency.incompatible && provided:
				problems = append(problems, moduleProblemExpectation{
					owner:      fuzzProjectNames[declaration.project],
					dependency: dependency.id,
				})
			case !dependency.incompatible && !provided:
				problems = append(problems, moduleProblemExpectation{
					owner:      fuzzProjectNames[declaration.project],
					dependency: dependency.id,
				})
			}
		}
	}
	slices.SortFunc(problems, func(a, b moduleProblemExpectation) int {
		if order := strings.Compare(a.owner, b.owner); order != 0 {
			return order
		}
		return strings.Compare(a.dependency, b.dependency)
	})
	return problems, nil
}

func matchModuleProblems(
	expected []moduleProblemExpectation,
	reported []resolve.ModuleProblem,
) error {
	if len(reported) != len(expected) {
		return fmt.Errorf("reported %d module problems, want %d: %v", len(reported), len(expected), reported)
	}
	used := make([]bool, len(reported))
	for _, want := range expected {
		found := false
		for index, problem := range reported {
			if used[index] || problem.ProjectID != want.owner {
				continue
			}
			if !strings.Contains(problem.Message, want.dependency) {
				continue
			}
			used[index] = true
			found = true
			break
		}
		if !found {
			return fmt.Errorf(
				"no module problem blames %s for %s in %v",
				want.owner,
				want.dependency,
				reported,
			)
		}
	}
	return nil
}

func collectModuleProblems(err error) []resolve.ModuleProblem {
	var problems []resolve.ModuleProblem
	var visit func(error)
	visit = func(current error) {
		if current == nil {
			return
		}
		if problem, ok := current.(resolve.ModuleProblem); ok {
			problems = append(problems, problem)
			return
		}
		switch joined := current.(type) {
		case interface{ Unwrap() []error }:
			for _, child := range joined.Unwrap() {
				visit(child)
			}
		case interface{ Unwrap() error }:
			visit(joined.Unwrap())
		}
	}
	visit(err)
	return problems
}

func validateModuleBindings(demand bool, out []document.Package) error {
	for _, pkg := range out {
		project := slices.Index(fuzzProjectNames[:], pkg.ProjectID)
		if project < 0 {
			return fmt.Errorf("selected unknown project %q", pkg.ProjectID)
		}
		if !pkg.Enabled() {
			return fmt.Errorf(
				"selected %s is not enabled for installation; requirements = %#v",
				pkg.ProjectID,
				pkg.Requirements,
			)
		}
		name := fuzzProjectNames[project]
		if demand && project == 1 {
			// The disabled direct declaration stays on the package alongside
			// the dependency demand that selected it.
			direct := document.RequirementBinding{
				Reference: "modrinth:" + name,
				Selector:  versionpkg.Any,
				Enabled:   false,
			}
			if !slices.Contains(pkg.Requirements, direct) {
				return fmt.Errorf(
					"selected %s lost its disabled direct binding; requirements = %#v",
					pkg.ProjectID,
					pkg.Requirements,
				)
			}
			continue
		}
		want := document.RequirementBinding{
			Reference: "modrinth:" + name,
			Selector:  versionpkg.Any,
			Enabled:   true,
		}
		if !slices.Contains(pkg.Requirements, want) {
			return fmt.Errorf(
				"selected %s lost its direct binding %#v; requirements = %#v",
				pkg.ProjectID,
				want,
				pkg.Requirements,
			)
		}
	}
	return nil
}

func validateModuleOutput(out []document.Package, expected map[int][]moduleKey) error {
	seen := make(map[int]bool, len(fuzzProjectNames))
	for _, pkg := range out {
		project := slices.Index(fuzzProjectNames[:], pkg.ProjectID)
		if project < 0 {
			return fmt.Errorf("selected unknown project %q", pkg.ProjectID)
		}
		if seen[project] {
			return fmt.Errorf("selected project %q more than once", pkg.ProjectID)
		}
		seen[project] = true
		actual := make([]moduleKey, 0, len(pkg.Modules))
		for _, module := range pkg.Modules {
			actual = append(actual, moduleKey{
				id:      module.ID,
				version: string(module.Version),
				nested:  module.NestedPath,
			})
		}
		if !slices.Equal(actual, expected[project]) {
			return fmt.Errorf(
				"project %s modules = %#v, want %#v",
				pkg.ProjectID,
				actual,
				expected[project],
			)
		}
	}
	for project, name := range fuzzProjectNames {
		if !seen[project] {
			return fmt.Errorf("project %s is missing from the output", name)
		}
	}
	return nil
}
