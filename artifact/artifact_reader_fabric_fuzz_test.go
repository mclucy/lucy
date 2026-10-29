package artifact_test

import (
	"strings"
	"testing"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/version"
)

type fabricFixtureDescriptor struct {
	ID          string             `json:"id"`
	Version     string             `json:"version"`
	Environment string             `json:"environment,omitempty"`
	Provides    []string           `json:"provides,omitempty"`
	Jars        []fabricFixtureJar `json:"jars,omitempty"`
	Depends     map[string]any     `json:"depends,omitempty"`
	Recommends  map[string]any     `json:"recommends,omitempty"`
	Suggests    map[string]any     `json:"suggests,omitempty"`
	Breaks      map[string]any     `json:"breaks,omitempty"`
	Conflicts   map[string]any     `json:"conflicts,omitempty"`
}

type fabricFixtureJar struct {
	File string `json:"file"`
}

func fabricNestedFixture(
	t testing.TB,
	id, version, environment string,
	provides []string,
	constraint string,
) []byte {
	t.Helper()
	return zipFixture(t, zipFixtureEntry{
		name: "fabric.mod.json",
		data: jsonFixture(t, fabricFixtureDescriptor{
			ID:          id,
			Version:     version,
			Environment: environment,
			Provides:    provides,
			Depends: map[string]any{
				"inner-required": constraint,
			},
			Suggests: map[string]any{
				"inner-suggested": constraint,
			},
		}),
	})
}

// FuzzReadInfoFabric cross-checks the package view from ReadInfo with the
// module inventory from ReadModules on the same archive. The archive holds an
// outer Fabric module plus one nested/module-container layout selected by the
// fuzzed case.
func FuzzReadInfoFabric(f *testing.F) {
	f.Add("example", "1.0.0", "2.0.0", "gamma", ">=1.0.0 <2.0.0", "server", 0)
	f.Add("example", "1.0.0", "2.0.0", "gamma", "*", "client", 1)
	f.Add("example", "1.0.0", "2.0.0", "delta", ">=1.0.0", "server", 1)
	f.Add("example", "", "", "delta", "1.2.3 || >=2.0.0", "both", 2)
	f.Add("example", "1.0.0", "2.0.0", "delta", "1.x", "server", 3)
	f.Add("example", "1.0.0", "2.0.0", "delta", ">=1.0.0", "server", 4)
	f.Add("example", "1.0.0", "2.0.0", "delta", "~1.2", "server", 5)
	f.Add("dependency-gamma", "1.0.0", "2.0.0", "gamma", ">=1.0.0", "server", 5)
	f.Add("example", "1.0.0", "2.0.0", "delta", "<=2.0.0", "server", 6)
	f.Add("example", "1.0.0", "2.0.0", "delta", "not a constraint", "server", 0)
	f.Add("example", "1.0.0", "2.0.0", "delta", "1.0.0 || *", "server", 0)
	f.Add("example", "1.0.0", "2.0.0", "delta", ">foo", "server", 0)
	f.Add("example", "1.0.0", "2.0.0", "delta", "", "server", 4)

	path := fuzzArchivePath(f, "artifact.jar")
	f.Fuzz(func(
		t *testing.T,
		rawID, rawVersion, rawNestedVersion, rawDependencyID, rawConstraint, rawSide string,
		rawNestedCase int,
	) {
		modID := safeID(rawID, "example-mod")
		modVersion := clippedText(rawVersion, 64)
		if strings.TrimSpace(modVersion) == "" {
			modVersion = "1.0.0"
		}
		nestedVersion := clippedText(rawNestedVersion, 64)
		if strings.TrimSpace(nestedVersion) == "" {
			nestedVersion = "2.0.0"
		}
		dependencyID := "dependency-" + safeID(rawDependencyID, "gamma")
		constraint := clippedText(rawConstraint, 128)
		side := clippedText(rawSide, 16)
		nestedCase := modulo(rawNestedCase, 7)

		jarPath := ""
		var nestedArchive []byte
		switch nestedCase {
		case 0: // descriptor-bearing nested mod
			jarPath = "nested/embedded.jar"
			nestedArchive = fabricNestedFixture(
				t, "embedded-mod", nestedVersion, "server",
				[]string{"nested-provided-zeta", "nested-provided-alpha"},
				constraint,
			)
		case 1: // nested mod restricted to the client
			jarPath = "nested/embedded.jar"
			nestedArchive = fabricNestedFixture(
				t, "embedded-mod", nestedVersion, "client",
				[]string{"nested-provided-zeta", "nested-provided-alpha"},
				constraint,
			)
		case 2: // dangling jars reference
			jarPath = "nested/embedded.jar"
		case 3: // nested entry is not a ZIP
			jarPath = "nested/embedded.jar"
			nestedArchive = []byte("not a zip archive")
		case 4: // nested ZIP without a fabric descriptor
			jarPath = "embedded-mod.jar"
			nestedArchive = zipFixture(t, zipFixtureEntry{
				name: "META-INF/MANIFEST.MF",
				data: []byte("Manifest-Version: 1.0\n"),
			})
		case 5: // nested descriptor reuses the outer ID and provides a dep
			jarPath = "nested/collision.jar"
			nestedArchive = fabricNestedFixture(
				t, modID, nestedVersion, "server",
				[]string{"nested-provided-zeta", "embedded-mod", "nested-provided-alpha"},
				constraint,
			)
		case 6: // nested descriptor is invalid JSON
			jarPath = "nested/broken.jar"
			nestedArchive = zipFixture(t, zipFixtureEntry{
				name: "fabric.mod.json",
				data: []byte("{not valid json"),
			})
		}

		outer := fabricFixtureDescriptor{
			ID:          modID,
			Version:     modVersion,
			Environment: side,
			Provides:    []string{"provided-zeta", "provided-alpha"},
			Jars:        []fabricFixtureJar{{File: jarPath}},
			Depends: map[string]any{
				dependencyID:   constraint,
				"minecraft":    "*",
				"java":         "*",
				"fabricloader": "*",
				"embedded-mod": "*",
			},
			Recommends: map[string]any{
				"recommended-" + dependencyID: []string{constraint},
			},
			Suggests: map[string]any{
				"suggested-" + dependencyID: constraint,
			},
			Breaks: map[string]any{
				"breaks-" + dependencyID: constraint,
			},
			Conflicts: map[string]any{
				"conflicts-" + dependencyID: constraint,
			},
		}
		entries := []zipFixtureEntry{{
			name: "fabric.mod.json",
			data: jsonFixture(t, outer),
		}}
		if nestedCase != 2 {
			entries = append(entries, zipFixtureEntry{name: jarPath, data: nestedArchive})
		}
		writeZipFixture(t, path, entries...)

		infos, readInfoErr := artifact.ReadInfo(path)
		modules, readModulesErr := artifact.ReadModules(
			t.Context(), path, catalog.EcosystemFabric,
		)

		if catalog.ParseSide(side) == catalog.SideClient {
			// ReadModules excludes client-only descriptors from the deployable
			// inventory. ReadInfo is metadata-only, so its result is not
			// asserted here.
			if readModulesErr != nil {
				t.Fatalf("ReadModules() error = %v", readModulesErr)
			}
			if len(modules) != 0 {
				t.Fatalf("client-only module yielded inventory: %+v", modules)
			}
			return
		}

		engine, err := version.NewEngine(version.DialectFabric)
		if err != nil {
			t.Fatalf("NewEngine(fabric) error = %v", err)
		}
		baseConstraint, constraintErr := engine.ParseConstraint(constraint)

		if nestedCase == 2 || nestedCase == 3 || nestedCase == 6 {
			// Missing or corrupt nested containers are surfaced by the module
			// inventory. ReadInfo's tolerant metadata behavior is not asserted.
			if readModulesErr == nil {
				t.Fatalf("ReadModules() succeeded with broken nested archive")
			}
			return
		}

		// Package translation parses ranges, while the module inventory keeps
		// descriptor text intact. Assert package contracts only for non-empty
		// parseable ranges; module contracts are checked below for both.
		if constraintErr == nil && strings.TrimSpace(constraint) != "" {
			if readInfoErr != nil {
				t.Fatalf("ReadInfo() error = %v", readInfoErr)
			}
			if len(infos) != 1 {
				t.Fatalf("ReadInfo() packages = %d, want 1: %+v", len(infos), infos)
			}
			info := infos[0]
			if got := string(info.Ref.Name); got != modID {
				t.Fatalf("package identity = %q, want %q", got, modID)
			}
			if info.Ref.Version != version.Version(modVersion) {
				t.Fatalf("package version = %q, want %q", info.Ref.Version, modVersion)
			}
			if info.Ref.Ecosystem != catalog.EcosystemFabric {
				t.Fatalf("package ecosystem = %q, want fabric", info.Ref.Ecosystem)
			}
			if info.Ref.Provider != catalog.ProviderUnknown {
				t.Fatalf("package provider = %q, want unknown", info.Ref.Provider)
			}
			if info.FilePath != path {
				t.Fatalf("package path = %q, want %q", info.FilePath, path)
			}
			expectedDependencies := map[string]expectedArtifactDependency{
				dependencyID: {
					kind: catalog.DependencyKindRequired, mandatory: true,
				},
				"minecraft": {
					kind: catalog.DependencyKindRequired, mandatory: true, wildcard: true,
				},
				"java": {
					kind: catalog.DependencyKindRequired, mandatory: true, wildcard: true,
				},
				"fabricloader": {
					kind: catalog.DependencyKindRequired, mandatory: true, wildcard: true,
				},
				"embedded-mod": {
					kind: catalog.DependencyKindRequired, mandatory: true, wildcard: true,
				},
				"recommended-" + dependencyID: {
					kind: catalog.DependencyKindRecommended,
				},
				"suggested-" + dependencyID: {
					kind: catalog.DependencyKindSuggested,
				},
				"breaks-" + dependencyID: {
					kind: catalog.DependencyKindIncompatible, negated: true,
				},
				"conflicts-" + dependencyID: {
					kind: catalog.DependencyKindConflict, negated: true,
				},
			}
			embeddedDependencies := make(map[string]struct{}, 2)
			switch nestedCase {
			case 0, 1, 4:
				embeddedDependencies["embedded-mod"] = struct{}{}
			case 5:
				embeddedDependencies["embedded-mod"] = struct{}{}
				embeddedDependencies[modID] = struct{}{}
			}
			for id, want := range expectedDependencies {
				switch {
				case id == "minecraft" || id == "java" || id == "fabricloader":
					want.origin = catalog.DependencyServerProvided
				default:
					if _, embedded := embeddedDependencies[id]; embedded {
						want.origin = catalog.DependencyEmbedded
					} else {
						want.origin = catalog.DependencyRegular
					}
				}
				expectedDependencies[id] = want
			}
			checkFabricPackageDependencies(t, engine, baseConstraint, info.Dependencies, expectedDependencies)
		}

		if readModulesErr != nil {
			t.Fatalf("ReadModules() error = %v", readModulesErr)
		}
		nestedIncluded := nestedCase == 0 || nestedCase == 5
		wantModules := 1
		if nestedIncluded {
			wantModules = 2
		}
		if len(modules) != wantModules {
			t.Fatalf("ReadModules() modules = %d, want %d: %+v", len(modules), wantModules, modules)
		}
		checkFabricModule(t, modules[0], fabricModuleExpectation{
			id:       modID,
			version:  modVersion,
			provides: []string{"provided-alpha", "provided-zeta"},
			path:     "",
			dependencies: map[string]expectedModuleDependency{
				dependencyID:   {kind: catalog.DependencyKindRequired, constraint: constraint},
				"minecraft":    {kind: catalog.DependencyKindRequired, constraint: "*"},
				"java":         {kind: catalog.DependencyKindRequired, constraint: "*"},
				"fabricloader": {kind: catalog.DependencyKindRequired, constraint: "*"},
				"embedded-mod": {kind: catalog.DependencyKindRequired, constraint: "*"},
				"recommended-" + dependencyID: {
					kind: catalog.DependencyKindRecommended, constraint: constraint,
				},
				"suggested-" + dependencyID: {
					kind: catalog.DependencyKindSuggested, constraint: constraint,
				},
				"breaks-" + dependencyID: {
					kind: catalog.DependencyKindIncompatible, constraint: constraint,
				},
				"conflicts-" + dependencyID: {
					kind: catalog.DependencyKindConflict, constraint: constraint,
				},
			},
		})
		if !nestedIncluded {
			return
		}
		nestedID := "embedded-mod"
		nestedProvides := []string{"nested-provided-alpha", "nested-provided-zeta"}
		if nestedCase == 5 {
			nestedID = modID
			nestedProvides = []string{
				"embedded-mod", "nested-provided-alpha", "nested-provided-zeta",
			}
		}
		checkFabricModule(t, modules[1], fabricModuleExpectation{
			id:       nestedID,
			version:  nestedVersion,
			provides: nestedProvides,
			path:     jarPath,
			dependencies: map[string]expectedModuleDependency{
				"inner-required": {
					kind: catalog.DependencyKindRequired, constraint: constraint,
				},
				"inner-suggested": {
					kind: catalog.DependencyKindSuggested, constraint: constraint,
				},
			},
		})
	})
}

type expectedArtifactDependency struct {
	kind      catalog.DependencyKind
	mandatory bool
	origin    catalog.DependencyOrigin
	negated   bool
	wildcard  bool
}

func checkFabricPackageDependencies(
	t *testing.T,
	engine version.Engine,
	base version.Expr,
	dependencies []artifact.Dependency,
	expected map[string]expectedArtifactDependency,
) {
	t.Helper()
	if len(dependencies) != len(expected) {
		t.Fatalf("dependencies = %d, want %d: %+v", len(dependencies), len(expected), dependencies)
	}
	probes := []version.Version{"0.0.0", "1.0.0", "1.2.3", "1.5.0", "2.0.0", "10.0.0"}
	seen := make(map[string]struct{}, len(dependencies))
	for _, dependency := range dependencies {
		id := string(dependency.Ref.Name)
		want, ok := expected[id]
		if !ok {
			t.Fatalf("unexpected dependency %q", id)
		}
		if _, duplicate := seen[id]; duplicate {
			t.Fatalf("duplicate dependency %q", id)
		}
		seen[id] = struct{}{}
		if dependency.Kind != want.kind {
			t.Fatalf("dependency %q kind = %q, want %q", id, dependency.Kind, want.kind)
		}
		if dependency.Mandatory != want.mandatory {
			t.Fatalf("dependency %q mandatory = %v, want %v", id, dependency.Mandatory, want.mandatory)
		}
		if dependency.Origin != want.origin {
			t.Fatalf("dependency %q origin = %q, want %q", id, dependency.Origin, want.origin)
		}
		if dependency.Ref.Ecosystem != catalog.EcosystemFabric {
			t.Fatalf("dependency %q ecosystem = %q, want fabric", id, dependency.Ref.Ecosystem)
		}
		for _, rawProbe := range probes {
			probe, err := engine.ParseVersion(rawProbe)
			if err != nil {
				t.Fatalf("ParseVersion(%q) error = %v", rawProbe, err)
			}
			expectedConstraint := base
			if want.wildcard {
				expectedConstraint = nil
			}
			wantMatch := engine.Match(expectedConstraint, probe)
			if want.negated {
				wantMatch = !wantMatch
			}
			if got := engine.Match(dependency.Constraint, probe); got != wantMatch {
				t.Fatalf(
					"dependency %q constraint match(%s) = %v, want %v",
					id, rawProbe, got, wantMatch,
				)
			}
		}
	}
}

type expectedModuleDependency struct {
	kind       catalog.DependencyKind
	constraint string
}

type fabricModuleExpectation struct {
	id           string
	version      string
	provides     []string
	path         string
	dependencies map[string]expectedModuleDependency
}

func checkFabricModule(
	t *testing.T,
	module catalog.DescriptorModule,
	expected fabricModuleExpectation,
) {
	t.Helper()
	if module.ID != expected.id {
		t.Fatalf("module ID = %q, want %q", module.ID, expected.id)
	}
	if module.Version != version.Version(expected.version) {
		t.Fatalf("module %q version = %q, want %q", module.ID, module.Version, expected.version)
	}
	if module.Ecosystem != catalog.EcosystemFabric {
		t.Fatalf("module %q ecosystem = %q, want fabric", module.ID, module.Ecosystem)
	}
	if module.VersionDialect != version.DialectFabric {
		t.Fatalf("module %q dialect = %q, want fabric", module.ID, module.VersionDialect)
	}
	if module.NestedPath != expected.path {
		t.Fatalf("module %q nested path = %q, want %q", module.ID, module.NestedPath, expected.path)
	}
	if module.DescriptorPath != "fabric.mod.json" {
		t.Fatalf("module %q descriptor path = %q, want fabric.mod.json", module.ID, module.DescriptorPath)
	}
	if len(module.Provides) != len(expected.provides) {
		t.Fatalf("module %q provides = %v, want %v", module.ID, module.Provides, expected.provides)
	}
	for index, provided := range expected.provides {
		if module.Provides[index] != provided {
			t.Fatalf("module %q provides = %v, want %v", module.ID, module.Provides, expected.provides)
		}
	}
	if len(module.Dependencies) != len(expected.dependencies) {
		t.Fatalf(
			"module %q dependencies = %d, want %d: %+v",
			module.ID, len(module.Dependencies), len(expected.dependencies), module.Dependencies,
		)
	}
	seen := make(map[string]struct{}, len(module.Dependencies))
	for _, dependency := range module.Dependencies {
		want, ok := expected.dependencies[dependency.ID]
		if !ok {
			t.Fatalf("module %q unexpected dependency %q", module.ID, dependency.ID)
		}
		if _, duplicate := seen[dependency.ID]; duplicate {
			t.Fatalf("module %q duplicate dependency %q", module.ID, dependency.ID)
		}
		seen[dependency.ID] = struct{}{}
		if dependency.Kind != want.kind {
			t.Fatalf(
				"module %q dependency %q kind = %q, want %q",
				module.ID, dependency.ID, dependency.Kind, want.kind,
			)
		}
		if dependency.Constraint != want.constraint {
			t.Fatalf(
				"module %q dependency %q constraint = %q, want %q",
				module.ID, dependency.ID, dependency.Constraint, want.constraint,
			)
		}
	}
}
