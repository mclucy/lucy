package resolve

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	serverpkg "github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
)

func quiltTestModule(
	id string,
	version string,
	ecosystem catalog.Ecosystem,
	dependencies ...catalog.DescriptorDependency,
) catalog.DescriptorModule {
	return catalog.DescriptorModule{
		ID:             id,
		Version:        versionpkg.Version(version),
		Ecosystem:      ecosystem,
		VersionDialect: versionpkg.DialectFabric,
		Dependencies:   dependencies,
	}
}

func quiltTestPackage(
	ecosystem catalog.Ecosystem,
	projectID string,
	modules ...catalog.DescriptorModule,
) document.Package {
	return document.Package{
		Environment: serverpkg.EnvironmentServer,
		Ecosystem:   ecosystem,
		Artifact: catalog.Artifact{
			Provider:  catalog.ProviderModrinth,
			ProjectID: projectID,
			Filename:  projectID + ".jar",
			Modules:   modules,
		},
	}
}

func quiltTestTarget(coreModules ...catalog.DescriptorModule) Target {
	return Target{
		Product:     serverpkg.ProductQuilt,
		Minecraft:   versionpkg.Version("1.21.1"),
		CoreVersion: versionpkg.Version("0.27.0"),
		CoreModules: coreModules,
	}
}

func quiltTestDependency(
	id string,
	constraint string,
	kind catalog.DependencyKind,
) catalog.DescriptorDependency {
	return catalog.DescriptorDependency{
		ID:         id,
		Constraint: constraint,
		Kind:       kind,
	}
}

func requireQuiltProblems(
	t *testing.T,
	problems []ModuleProblem,
	want int,
) {
	t.Helper()
	if len(problems) != want {
		t.Fatalf("problems = %+v, want %d", problems, want)
	}
}

func quiltTestModuleByID(
	t *testing.T,
	modules []catalog.DescriptorModule,
	id string,
) catalog.DescriptorModule {
	t.Helper()
	for _, module := range modules {
		if module.ID == id {
			return module
		}
	}
	t.Fatalf("module %q not found in %+v", id, modules)
	return catalog.DescriptorModule{}
}

func quiltCrossPackages() []document.Package {
	return []document.Package{
		quiltTestPackage(
			catalog.EcosystemQuilt,
			"quilt-project",
			quiltTestModule(
				"quilt-core",
				"1.0.0",
				catalog.EcosystemQuilt,
				quiltTestDependency(
					"fabric-lib",
					"",
					catalog.DependencyKindRequired,
				),
			),
		),
		quiltTestPackage(
			catalog.EcosystemFabric,
			"fabric-project",
			quiltTestModule(
				"fabric-lib",
				"1.0.0",
				catalog.EcosystemFabric,
			),
			quiltTestModule(
				"fabric-user",
				"1.0.0",
				catalog.EcosystemFabric,
				quiltTestDependency(
					"quilt-core",
					"",
					catalog.DependencyKindRequired,
				),
			),
		),
	}
}

// TestFinalizeModulesQuiltSharesModuleNamespace covers model-level Fabric and
// Quilt module dependency resolution in both directions under a Quilt target.
func TestFinalizeModulesQuiltSharesModuleNamespace(t *testing.T) {
	t.Run("quilt target resolves both directions", func(t *testing.T) {
		packages := quiltCrossPackages()
		problems, err := finalizeModules(packages, quiltTestTarget())
		if err != nil {
			t.Fatalf("finalizeModules() error = %v", err)
		}
		requireQuiltProblems(t, problems, 0)

		quiltCore := quiltTestModuleByID(
			t,
			packages[0].Modules,
			"quilt-core",
		)
		fabricDep := quiltCore.Dependencies[0]
		if fabricDep.ModuleID != "fabric-lib" ||
			fabricDep.ProjectID != "fabric-project" ||
			fabricDep.Provider != catalog.ProviderModrinth {
			t.Fatalf("quilt dependency binding = %+v", fabricDep)
		}

		fabricUser := quiltTestModuleByID(
			t,
			packages[1].Modules,
			"fabric-user",
		)
		quiltDep := fabricUser.Dependencies[0]
		if quiltDep.ModuleID != "quilt-core" ||
			quiltDep.ProjectID != "quilt-project" ||
			quiltDep.Provider != catalog.ProviderModrinth {
			t.Fatalf("fabric dependency binding = %+v", quiltDep)
		}
	})

	t.Run("fabric target keeps the namespaces apart", func(t *testing.T) {
		packages := quiltCrossPackages()
		target := Target{
			Product:     serverpkg.ProductFabric,
			Minecraft:   versionpkg.Version("1.21.1"),
			CoreVersion: versionpkg.Version("0.16.0"),
		}
		problems, err := finalizeModules(packages, target)
		if err != nil {
			t.Fatalf("finalizeModules() error = %v", err)
		}
		requireQuiltProblems(t, problems, 2)
		if problems[0].RequiredBy != "quilt-core" ||
			problems[0].Dependency != "fabric-lib" {
			t.Fatalf("first problem = %+v", problems[0])
		}
		if problems[1].RequiredBy != "fabric-user" ||
			problems[1].Dependency != "quilt-core" {
			t.Fatalf("second problem = %+v", problems[1])
		}
	})
}

func TestFinalizeModulesQuiltOptionalPresence(t *testing.T) {
	tests := []struct {
		name       string
		libVersion string
		want       int
	}{
		{name: "absent optional is not required", libVersion: "", want: 0},
		{
			name:       "present mismatched optional fails",
			libVersion: "1.0.0",
			want:       1,
		},
		{
			name:       "present matching optional passes",
			libVersion: "2.1.0",
			want:       0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			optional := quiltTestDependency(
				"lib",
				">=2.0.0",
				catalog.DependencyKindOptional,
			)
			packages := []document.Package{
				quiltTestPackage(
					catalog.EcosystemQuilt,
					"consumer-project",
					quiltTestModule(
						"consumer",
						"1.0.0",
						catalog.EcosystemQuilt,
						optional,
					),
				),
			}
			if tt.libVersion != "" {
				packages = append(packages, quiltTestPackage(
					catalog.EcosystemQuilt,
					"lib-project",
					quiltTestModule(
						"lib",
						tt.libVersion,
						catalog.EcosystemQuilt,
					),
				))
			}

			problems, err := finalizeModules(packages, quiltTestTarget())
			if err != nil {
				t.Fatalf("finalizeModules() error = %v", err)
			}
			requireQuiltProblems(t, problems, tt.want)
			if tt.want == 1 {
				if problems[0].Dependency != "lib" ||
					problems[0].Selected != "1.0.0" ||
					!strings.Contains(problems[0].Message, "optionally") {
					t.Fatalf("problem = %+v", problems[0])
				}
			}
			if tt.want == 0 && tt.libVersion != "" {
				consumer := quiltTestModuleByID(
					t,
					packages[0].Modules,
					"consumer",
				)
				if consumer.Dependencies[0].ModuleID != "lib" {
					t.Fatalf(
						"dependency binding = %+v",
						consumer.Dependencies[0],
					)
				}
			}
		})
	}
}

func TestFinalizeModulesQuiltAnyOf(t *testing.T) {
	consumerDependency := func() catalog.DescriptorDependency {
		dependency := quiltTestDependency(
			"alt-a",
			">=2.0.0",
			catalog.DependencyKindRequired,
		)
		dependency.AnyOf = []catalog.DescriptorDependency{
			quiltTestDependency(
				"alt-b",
				">=1.0.0",
				catalog.DependencyKindRequired,
			),
		}
		return dependency
	}
	tests := []struct {
		name        string
		altAVersion string
		altBVersion string
		want        int
		wantModule  string
	}{
		{
			name:        "alternative matches",
			altBVersion: "1.5.0",
			want:        0,
			wantModule:  "alt-b",
		},
		{
			name:        "primary matches",
			altAVersion: "2.1.0",
			want:        0,
			wantModule:  "alt-a",
		},
		{
			name:        "present alternative wins over mismatched primary",
			altAVersion: "1.0.0",
			altBVersion: "1.5.0",
			want:        0,
			wantModule:  "alt-b",
		},
		{
			name:        "no alternative matches",
			altAVersion: "1.0.0",
			want:        1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packages := []document.Package{
				quiltTestPackage(
					catalog.EcosystemQuilt,
					"consumer-project",
					quiltTestModule(
						"consumer",
						"1.0.0",
						catalog.EcosystemQuilt,
						consumerDependency(),
					),
				),
			}
			for _, fixture := range []struct {
				id      string
				version string
			}{
				{id: "alt-a", version: tt.altAVersion},
				{id: "alt-b", version: tt.altBVersion},
			} {
				if fixture.version == "" {
					continue
				}
				packages = append(packages, quiltTestPackage(
					catalog.EcosystemQuilt,
					fixture.id+"-project",
					quiltTestModule(
						fixture.id,
						fixture.version,
						catalog.EcosystemQuilt,
					),
				))
			}

			problems, err := finalizeModules(packages, quiltTestTarget())
			if err != nil {
				t.Fatalf("finalizeModules() error = %v", err)
			}
			requireQuiltProblems(t, problems, tt.want)
			if tt.want == 1 {
				if !strings.Contains(problems[0].Message, "one of") {
					t.Fatalf("problem = %+v", problems[0])
				}
				return
			}
			consumer := quiltTestModuleByID(
				t,
				packages[0].Modules,
				"consumer",
			)
			if got := consumer.Dependencies[0].ModuleID; got != tt.wantModule {
				t.Fatalf(
					"dependency module = %q, want %q",
					got,
					tt.wantModule,
				)
			}
		})
	}
}

func TestFinalizeModulesQuiltAnyOfUnless(t *testing.T) {
	packages := func(indium bool) []document.Package {
		unless := []catalog.DescriptorDependency{
			quiltTestDependency(
				"indium",
				"",
				catalog.DependencyKindRequired,
			),
		}
		dependency := quiltTestDependency(
			"alt-a",
			"",
			catalog.DependencyKindRequired,
		)
		dependency.Unless = unless
		dependency.AnyOf = []catalog.DescriptorDependency{
			quiltTestDependency(
				"alt-b",
				"",
				catalog.DependencyKindRequired,
			),
		}
		out := []document.Package{
			quiltTestPackage(
				catalog.EcosystemQuilt,
				"consumer-project",
				quiltTestModule(
					"consumer",
					"1.0.0",
					catalog.EcosystemQuilt,
					dependency,
				),
			),
		}
		if indium {
			out = append(out, quiltTestPackage(
				catalog.EcosystemQuilt,
				"indium-project",
				quiltTestModule(
					"indium",
					"1.0.0",
					catalog.EcosystemQuilt,
				),
			))
		}
		return out
	}

	with, err := finalizeModules(packages(true), quiltTestTarget())
	if err != nil {
		t.Fatalf("finalizeModules() error = %v", err)
	}
	requireQuiltProblems(t, with, 0)

	without, err := finalizeModules(packages(false), quiltTestTarget())
	if err != nil {
		t.Fatalf("finalizeModules() error = %v", err)
	}
	requireQuiltProblems(t, without, 1)
}

func TestFinalizeModulesQuiltAnyOfMixedOptional(t *testing.T) {
	consumerDependency := func() catalog.DescriptorDependency {
		dependency := quiltTestDependency(
			"alt-a",
			">=2.0.0",
			catalog.DependencyKindOptional,
		)
		dependency.AnyOf = []catalog.DescriptorDependency{
			quiltTestDependency(
				"alt-b",
				"",
				catalog.DependencyKindRequired,
			),
		}
		return dependency
	}
	tests := []struct {
		name        string
		altAVersion string
		altBVersion string
		want        int
		wantModule  string
	}{
		{
			name: "absent optional alternative satisfies the group",
			want: 0,
		},
		{
			name:        "present mismatched optional alternative does not satisfy",
			altAVersion: "1.0.0",
			want:        1,
		},
		{
			name:        "required alternative can still match",
			altAVersion: "1.0.0",
			altBVersion: "1.0.0",
			want:        0,
			wantModule:  "alt-b",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packages := []document.Package{
				quiltTestPackage(
					catalog.EcosystemQuilt,
					"consumer-project",
					quiltTestModule(
						"consumer",
						"1.0.0",
						catalog.EcosystemQuilt,
						consumerDependency(),
					),
				),
			}
			for _, fixture := range []struct {
				id      string
				version string
			}{
				{id: "alt-a", version: tt.altAVersion},
				{id: "alt-b", version: tt.altBVersion},
			} {
				if fixture.version == "" {
					continue
				}
				packages = append(packages, quiltTestPackage(
					catalog.EcosystemQuilt,
					fixture.id+"-project",
					quiltTestModule(
						fixture.id,
						fixture.version,
						catalog.EcosystemQuilt,
					),
				))
			}

			problems, err := finalizeModules(packages, quiltTestTarget())
			if err != nil {
				t.Fatalf("finalizeModules() error = %v", err)
			}
			requireQuiltProblems(t, problems, tt.want)
			if tt.want == 1 {
				if problems[0].Dependency != "alt-a" ||
					problems[0].Selected != "1.0.0" {
					t.Fatalf("problem = %+v", problems[0])
				}
				return
			}
			consumer := quiltTestModuleByID(
				t,
				packages[0].Modules,
				"consumer",
			)
			if got := consumer.Dependencies[0].ModuleID; got != tt.wantModule {
				t.Fatalf(
					"dependency module = %q, want %q",
					got,
					tt.wantModule,
				)
			}
		})
	}
}

func TestFinalizeModulesAdvisoryKindsAreNotEnforced(t *testing.T) {
	tests := []struct {
		name    string
		kind    catalog.DependencyKind
		version string
	}{
		{
			name:    "conflict",
			kind:    catalog.DependencyKindConflict,
			version: "1.0.0",
		},
		{
			name:    "suggested",
			kind:    catalog.DependencyKindSuggested,
			version: "1.0.0",
		},
		{
			name:    "recommended",
			kind:    catalog.DependencyKindRecommended,
			version: "1.0.0",
		},
		{
			name:    "conflict without a provider",
			kind:    catalog.DependencyKindConflict,
			version: "",
		},
		{
			name:    "suggested without a provider",
			kind:    catalog.DependencyKindSuggested,
			version: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packages := []document.Package{
				quiltTestPackage(
					catalog.EcosystemFabric,
					"c2me-project",
					quiltTestModule(
						"c2me",
						"1.0.0",
						catalog.EcosystemFabric,
						quiltTestDependency(
							"advisory",
							">=2.0.0",
							tt.kind,
						),
					),
				),
			}
			if tt.version != "" {
				packages = append(packages, quiltTestPackage(
					catalog.EcosystemFabric,
					"advisory-project",
					quiltTestModule(
						"advisory",
						tt.version,
						catalog.EcosystemFabric,
					),
				))
			}
			problems, err := finalizeModules(packages, quiltTestTarget())
			if err != nil {
				t.Fatalf("finalizeModules() error = %v", err)
			}
			requireQuiltProblems(t, problems, 0)
		})
	}

	t.Run("required stays enforced", func(t *testing.T) {
		packages := []document.Package{
			quiltTestPackage(
				catalog.EcosystemFabric,
				"c2me-project",
				quiltTestModule(
					"c2me",
					"1.0.0",
					catalog.EcosystemFabric,
					quiltTestDependency(
						"missing",
						"",
						catalog.DependencyKindRequired,
					),
				),
			),
		}
		problems, err := finalizeModules(packages, quiltTestTarget())
		if err != nil {
			t.Fatalf("finalizeModules() error = %v", err)
		}
		requireQuiltProblems(t, problems, 1)
	})
}

// quiltQSLRootDescriptor is the relevant portion of the real Quilted Fabric
// API 7.1.2+0.87.0-1.20.1 descriptor: the root unconditionally requires
// quilt_client_command, which only exists as a client-only nested jar.
const quiltQSLRootDescriptor = `{
  "schema_version": 1,
  "quilt_loader": {
    "group": "org.quiltmc",
    "id": "quilted_fabric_api",
    "version": "7.1.2+0.87.0-1.20.1",
    "metadata": {
      "name": "Quilted Fabric API",
      "license": "Apache-2.0"
    },
    "depends": [
      {"id": "quilt_loader", "versions": ">=0.19.2"},
      {"id": "java", "versions": ">=17"},
      {
        "id": "minecraft",
        "versions": {"all": [">=1.20-", "<1.20.2-"]}
      },
      "quilt_client_command",
      {"id": "quilt_client_only_dependency", "environment": "client"},
      "quilted_fabric_api_base"
    ],
    "provides": [{"id": "fabric-api", "version": "0.87.0"}],
    "jars": [
      "META-INF/jars/client_command-6.1.1+1.20.1.jar",
      "META-INF/jars/fabric-api-base-7.1.2+0.87.0-1.20.1.jar"
    ]
  }
}`

// quiltQSLClientCommandDescriptor is the real client-only nested descriptor
// shipped inside Quilted Fabric API. Its environment keeps it off a server,
// while the root descriptor still depends on its id.
const quiltQSLClientCommandDescriptor = `{
  "schema_version": 1,
  "quilt_loader": {
    "group": "org.quiltmc.qsl.management",
    "id": "quilt_client_command",
    "version": "6.1.1+1.20.1",
    "metadata": {
      "name": "Quilt Client Command API",
      "description": "Quilt APIs for creating and managing client-side commands.",
      "license": "Apache-2.0"
    },
    "intermediate_mappings": "net.fabricmc:intermediary",
    "load_type": "if_possible",
    "depends": [
      {"id": "quilt_loader", "versions": ">=0.19.2"},
      {
        "id": "minecraft",
        "versions": {"any": ["=1.20.1", "=1.20"]}
      },
      {"id": "quilt_base", "versions": ">=6.1.1+1.20.1"}
    ]
  },
  "mixin": "quilt_client_command.mixins.json",
  "minecraft": {"environment": "client"}
}`

const quiltQSLBaseDescriptor = `{
  "schema_version": 1,
  "quilt_loader": {
    "group": "org.quiltmc",
    "id": "quilted_fabric_api_base",
    "version": "7.1.2+0.87.0-1.20.1",
    "metadata": {"name": "Quilted Fabric API Base", "license": "Apache-2.0"}
  }
}`

func quiltTestZip(t *testing.T, entries map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := entry.Write(entries[name]); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buffer.Bytes()
}

func quiltQSLFixture(t *testing.T) string {
	t.Helper()
	clientCommand := quiltTestZip(t, map[string][]byte{
		"quilt.mod.json": []byte(quiltQSLClientCommandDescriptor),
	})
	base := quiltTestZip(t, map[string][]byte{
		"quilt.mod.json": []byte(quiltQSLBaseDescriptor),
	})
	archive := quiltTestZip(t, map[string][]byte{
		"quilt.mod.json": []byte(quiltQSLRootDescriptor),
		"META-INF/jars/client_command-6.1.1+1.20.1.jar":         clientCommand,
		"META-INF/jars/fabric-api-base-7.1.2+0.87.0-1.20.1.jar": base,
	})
	path := filepath.Join(t.TempDir(), "quilted-fabric-api.jar")
	if err := os.WriteFile(path, archive, 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func quiltModuleByID(
	t *testing.T,
	modules []catalog.DescriptorModule,
	id string,
) catalog.DescriptorModule {
	t.Helper()
	for _, module := range modules {
		if module.ID == id {
			return module
		}
	}
	t.Fatalf("module %q not found in %+v", id, modules)
	return catalog.DescriptorModule{}
}

// TestQuiltClientOnlyNestedDependencyCoverage reproduces the real Quilted
// Fabric API layout: a root descriptor depends on a module that only exists as
// a client-only nested jar. The server inventory drops that module, but its
// identity must keep the requirement from being reported missing.
func TestQuiltClientOnlyNestedDependencyCoverage(t *testing.T) {
	path := quiltQSLFixture(t)
	modules, err := artifact.ReadModules(
		context.Background(),
		path,
		catalog.EcosystemQuilt,
	)
	if err != nil {
		t.Fatalf("ReadModules() error = %v", err)
	}
	if slices.ContainsFunc(modules, func(m catalog.DescriptorModule) bool {
		return m.ID == "quilt_client_command"
	}) {
		t.Fatalf("client-only module survived into the server inventory: %+v", modules)
	}
	root := quiltModuleByID(t, modules, "quilted_fabric_api")
	if !slices.Contains(root.ClientOnly, "quilt_client_command") {
		t.Fatalf("root client-only identities = %v", root.ClientOnly)
	}
	quiltModuleByID(t, modules, "quilted_fabric_api_base")

	target := quiltTestTarget()
	target.Minecraft = versionpkg.Version("1.20.1")
	problems, err := finalizeModules(
		[]document.Package{
			quiltTestPackage(
				catalog.EcosystemQuilt,
				"quilted-fabric-api",
				modules...,
			),
		},
		target,
	)
	if err != nil {
		t.Fatalf("finalizeModules() error = %v", err)
	}
	requireQuiltProblems(t, problems, 0)

	// Without the client-only identity the same graph reports the requirement,
	// which proves the waiver is driven by the real descriptor data.
	stripped := make([]catalog.DescriptorModule, len(modules))
	copy(stripped, modules)
	for i := range stripped {
		stripped[i].ClientOnly = nil
	}
	problems, err = finalizeModules(
		[]document.Package{
			quiltTestPackage(
				catalog.EcosystemQuilt,
				"quilted-fabric-api",
				stripped...,
			),
		},
		target,
	)
	if err != nil {
		t.Fatalf("finalizeModules() error = %v", err)
	}
	requireQuiltProblems(t, problems, 1)
	if problems[0].Dependency != "quilt_client_command" {
		t.Fatalf("problem = %+v", problems[0])
	}
}

func quiltSubstitutionFixture() (document.Package, document.Package) {
	qfapi := quiltTestPackage(
		catalog.EcosystemQuilt,
		"qsl",
		quiltTestModule(
			"quilted_fabric_api",
			"7.1.2+0.87.0-1.20.1",
			catalog.EcosystemQuilt,
		),
	)
	qfapi.Modules[0].Provides = []string{"fabric-api", "fabric"}
	qfapi.Modules[0].ProvidedVersions = map[string]versionpkg.Version{
		"fabric-api": "0.87.0",
		"fabric":     "0.87.0",
	}
	qfapi.Requirements = []document.RequirementBinding{{
		Reference: "modrinth:qsl",
		Enabled:   true,
	}}

	fabricAPI := quiltTestPackage(
		catalog.EcosystemFabric,
		"fabric-api",
		quiltTestModule(
			"fabric-api",
			"0.83.0+1.20.1",
			catalog.EcosystemFabric,
		),
	)
	fabricAPI.Modules[0].Provides = []string{"fabric"}
	fabricAPI.Dependency = true
	fabricAPI.DependencyOf = []catalog.Ref{{
		Provider: catalog.ProviderModrinth,
		Name:     catalog.Name("friends-and-foes"),
	}}
	return qfapi, fabricAPI
}

func quiltBreakConsumer(constraint string) document.Package {
	return quiltTestPackage(
		catalog.EcosystemFabric,
		"voicechat",
		quiltTestModule(
			"voicechat",
			"1.0.0",
			catalog.EcosystemFabric,
			quiltTestDependency(
				"fabric-api",
				constraint,
				catalog.DependencyKindIncompatible,
			),
		),
	)
}

func TestProvidesFor(t *testing.T) {
	qfapi, fabricAPI := quiltSubstitutionFixture()

	partial := qfapi
	partial.Modules = []catalog.DescriptorModule{qfapi.Modules[0]}
	partial.Modules[0].Provides = []string{"fabric-api"}
	partial.Modules[0].ProvidedVersions = map[string]versionpkg.Version{
		"fabric-api": "0.87.0",
	}

	older := qfapi
	older.Modules = []catalog.DescriptorModule{qfapi.Modules[0]}
	older.Modules[0].ProvidedVersions = map[string]versionpkg.Version{
		"fabric-api": "0.80.0",
		"fabric":     "0.80.0",
	}
	newerConsumer := quiltTestPackage(
		catalog.EcosystemFabric,
		"consumer",
		quiltTestModule(
			"fabric-users",
			"1.0.0",
			catalog.EcosystemFabric,
			quiltTestDependency(
				"fabric-api",
				">=0.85.0",
				catalog.DependencyKindRequired,
			),
		),
	)

	fabricProvider := quiltTestPackage(
		catalog.EcosystemFabric,
		"fabric-impl",
		quiltTestModule("fabric-impl", "1.0.0", catalog.EcosystemFabric),
	)
	fabricProvider.Modules[0].Provides = qfapi.Modules[0].Provides
	fabricProvider.Modules[0].ProvidedVersions = qfapi.Modules[0].ProvidedVersions

	quiltTagged := fabricAPI
	quiltTagged.Ecosystem = catalog.EcosystemQuilt

	alpha := quiltTestPackage(
		catalog.EcosystemQuilt,
		"alpha-lib",
		quiltTestModule("shared-lib", "1.0.0", catalog.EcosystemQuilt),
	)
	beta := quiltTestPackage(
		catalog.EcosystemQuilt,
		"beta-lib",
		quiltTestModule("shared-lib", "1.0.0", catalog.EcosystemQuilt),
	)

	tests := []struct {
		name       string
		target     Target
		provider   document.Package
		dependency document.Package
		others     []document.Package
		want       bool
	}{
		{
			name:       "preferred descriptor covers every identity",
			target:     quiltTestTarget(),
			provider:   qfapi,
			dependency: fabricAPI,
			want:       true,
		},
		{
			name: "fabric target loads no Quilt provider",
			target: Target{
				Product:   serverpkg.ProductFabric,
				Minecraft: versionpkg.Version("1.20.1"),
			},
			provider:   qfapi,
			dependency: fabricAPI,
		},
		{
			name: "forge target loads neither package",
			target: Target{
				Product:   serverpkg.ProductForge,
				Minecraft: versionpkg.Version("1.20.1"),
			},
			provider:   qfapi,
			dependency: fabricAPI,
		},
		{
			name:       "incomplete identity coverage",
			target:     quiltTestTarget(),
			provider:   partial,
			dependency: fabricAPI,
		},
		{
			name:       "provided version fails a selected constraint",
			target:     quiltTestTarget(),
			provider:   older,
			dependency: fabricAPI,
			others:     []document.Package{newerConsumer},
		},
		{
			name:       "same descriptor precedence",
			target:     quiltTestTarget(),
			provider:   fabricProvider,
			dependency: fabricAPI,
		},
		{
			name:       "same descriptor precedence among Quilt packages",
			target:     quiltTestTarget(),
			provider:   alpha,
			dependency: beta,
		},
		{
			name:       "package ecosystem does not decide the descriptor",
			target:     quiltTestTarget(),
			provider:   qfapi,
			dependency: quiltTagged,
			want:       true,
		},
		{
			name:       "non-matching break permits substitution",
			target:     quiltTestTarget(),
			provider:   qfapi,
			dependency: fabricAPI,
			others:     []document.Package{quiltBreakConsumer("<0.83.0+1.20.1")},
			want:       true,
		},
		{
			name:       "matching break blocks substitution",
			target:     quiltTestTarget(),
			provider:   qfapi,
			dependency: fabricAPI,
			others:     []document.Package{quiltBreakConsumer("<0.90.0")},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			selected := append(
				[]document.Package{tt.provider, tt.dependency},
				tt.others...,
			)
			got := providesFor(tt.target, tt.provider, tt.dependency, selected)
			if got != tt.want {
				t.Fatalf("providesFor() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSupersedeProvidedDependency(t *testing.T) {
	qfapi, fabricAPI := quiltSubstitutionFixture()
	target := quiltTestTarget()
	qfapiKey := projectKey{
		serverpkg.EnvironmentServer,
		catalog.EcosystemQuilt,
		catalog.ProviderModrinth,
		"qsl",
	}
	fabricKey := projectKey{
		serverpkg.EnvironmentServer,
		catalog.EcosystemFabric,
		catalog.ProviderModrinth,
		"fabric-api",
	}

	t.Run("dependents move to the provider and back on restore", func(t *testing.T) {
		state := newSelectionState()
		state.install(fabricKey, fabricAPI, nil, false)
		branches := []selectionBranch{state.install(qfapiKey, qfapi, nil, false)}
		branches = append(branches, state.supersede(target, qfapiKey)...)
		if _, ok := state.selection(fabricKey); ok {
			t.Fatalf("superseded dependency stays selected")
		}
		provider, _ := state.selection(qfapiKey)
		if !provider.Dependency ||
			!slices.Contains(provider.DependencyOf, fabricAPI.DependencyOf[0]) {
			t.Fatalf("provider = %+v, want the dependency edge", provider)
		}
		state.restoreAll(branches)
		if _, ok := state.selection(fabricKey); !ok {
			t.Fatalf("restore did not reselect the dependency")
		}
		if _, ok := state.selection(qfapiKey); ok {
			t.Fatalf("restore kept the provider")
		}
	})

	t.Run("exact release demand is kept", func(t *testing.T) {
		state := newSelectionState()
		state.install(fabricKey, fabricAPI, nil, true)
		state.install(qfapiKey, qfapi, nil, false)
		if branches := state.supersede(target, qfapiKey); len(branches) != 0 {
			t.Fatalf("pinned dependency superseded")
		}
	})

	t.Run("merged exact release demand is kept", func(t *testing.T) {
		state := newSelectionState()
		state.install(fabricKey, fabricAPI, nil, false)
		merged := state.extend(fabricKey, fabricAPI, true)
		state.install(qfapiKey, qfapi, nil, false)
		if branches := state.supersede(target, qfapiKey); len(branches) != 0 {
			t.Fatalf("merged exact release demand superseded")
		}
		state.restore(merged)
		if branches := state.supersede(target, qfapiKey); len(branches) == 0 {
			t.Fatalf("unpinned dependency kept after the merge was abandoned")
		}
	})

	t.Run("abandoned pin does not survive its branch", func(t *testing.T) {
		state := newSelectionState()
		pinned := state.install(fabricKey, fabricAPI, nil, true)
		state.restore(pinned)
		state.install(fabricKey, fabricAPI, nil, false)
		state.install(qfapiKey, qfapi, nil, false)
		if branches := state.supersede(target, qfapiKey); len(branches) == 0 {
			t.Fatalf("unpinned dependency kept after the pin was abandoned")
		}
	})

	t.Run("direct requirement is kept", func(t *testing.T) {
		direct := fabricAPI
		direct.Dependency = false
		direct.Requirements = []document.RequirementBinding{{
			Reference: "modrinth:fabric-api",
			Enabled:   true,
		}}
		state := newSelectionState()
		state.install(fabricKey, direct, nil, false)
		state.install(qfapiKey, qfapi, nil, false)
		if branches := state.supersede(target, qfapiKey); len(branches) != 0 {
			t.Fatalf("direct requirement superseded")
		}
	})
}

// TestProvidedDependencyReplacesStandaloneModule reproduces the mixed
// Quilt/Fabric collision: a Fabric API dependency and Quilted Fabric API both
// answer the fabric module identity. Superseding the standalone dependency
// leaves one provider and lets the module requirement bind to it.
func TestProvidedDependencyReplacesStandaloneModule(t *testing.T) {
	qfapi, standalone := quiltSubstitutionFixture()
	fabricUser := quiltTestPackage(
		catalog.EcosystemFabric,
		"friends-and-foes",
		quiltTestModule(
			"friends_and_foes",
			"fabric-mc1.20.1-3.0.9",
			catalog.EcosystemFabric,
			quiltTestDependency(
				"fabric-api",
				">=0.83.0",
				catalog.DependencyKindRequired,
			),
		),
	)
	fabricUser.Requirements = []document.RequirementBinding{{
		Reference: "modrinth:friends-and-foes",
		Enabled:   true,
	}}

	both := []document.Package{qfapi, standalone, fabricUser}
	if _, err := finalizeModules(both, quiltTestTarget()); err == nil {
		t.Fatalf("graph with both providers did not report the duplicate native module")
	}

	state := newSelectionState()
	for _, p := range []document.Package{standalone, fabricUser, qfapi} {
		state.install(projectKey{
			p.Environment,
			p.Ecosystem,
			p.Provider,
			p.ProjectID,
		}, p, nil, false)
	}
	state.supersede(quiltTestTarget(), projectKey{
		qfapi.Environment,
		qfapi.Ecosystem,
		qfapi.Provider,
		qfapi.ProjectID,
	})
	packages := state.packages()
	sortPackages(packages)
	if len(packages) != 2 {
		t.Fatalf("selected packages = %+v", packages)
	}
	problems, err := finalizeModules(packages, quiltTestTarget())
	if err != nil {
		t.Fatalf("finalizeModules() error = %v", err)
	}
	requireQuiltProblems(t, problems, 0)

	user := quiltTestModuleByID(t, packages[0].Modules, "friends_and_foes")
	dependency := user.Dependencies[0]
	if dependency.ModuleID != "quilted_fabric_api" ||
		dependency.ProjectID != "qsl" {
		t.Fatalf("fabric-api dependency binding = %+v", dependency)
	}
}

func TestFinalizeModulesQuiltBreakAllOf(t *testing.T) {
	breakDependency := func() catalog.DescriptorDependency {
		dependency := quiltTestDependency(
			"old-a",
			"<2.0.0",
			catalog.DependencyKindIncompatible,
		)
		dependency.AllOf = []catalog.DescriptorDependency{
			quiltTestDependency(
				"old-b",
				"<2.0.0",
				catalog.DependencyKindIncompatible,
			),
		}
		return dependency
	}
	tests := []struct {
		name         string
		oldAVersion  string
		oldBVersion  string
		want         int
		wantSelected string
	}{
		{
			name:        "one conjunct is not a conflict",
			oldAVersion: "1.0.0",
			want:        0,
		},
		{
			name:         "all conjuncts match",
			oldAVersion:  "1.0.0",
			oldBVersion:  "1.0.0",
			want:         1,
			wantSelected: "1.0.0",
		},
		{
			name:        "primary outside its range is not a conflict",
			oldAVersion: "3.0.0",
			oldBVersion: "1.0.0",
			want:        0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packages := []document.Package{
				quiltTestPackage(
					catalog.EcosystemQuilt,
					"consumer-project",
					quiltTestModule(
						"consumer",
						"1.0.0",
						catalog.EcosystemQuilt,
						breakDependency(),
					),
				),
			}
			for _, fixture := range []struct {
				id      string
				version string
			}{
				{id: "old-a", version: tt.oldAVersion},
				{id: "old-b", version: tt.oldBVersion},
			} {
				if fixture.version == "" {
					continue
				}
				packages = append(packages, quiltTestPackage(
					catalog.EcosystemQuilt,
					fixture.id+"-project",
					quiltTestModule(
						fixture.id,
						fixture.version,
						catalog.EcosystemQuilt,
					),
				))
			}

			problems, err := finalizeModules(packages, quiltTestTarget())
			if err != nil {
				t.Fatalf("finalizeModules() error = %v", err)
			}
			requireQuiltProblems(t, problems, tt.want)
			if tt.want == 1 {
				if problems[0].Dependency != "old-a" ||
					problems[0].Selected != tt.wantSelected {
					t.Fatalf("problem = %+v", problems[0])
				}
			}
		})
	}
}

func TestFinalizeModulesQuiltUnless(t *testing.T) {
	t.Run("unless suppresses a break", func(t *testing.T) {
		packages := func(indium bool) []document.Package {
			dependency := quiltTestDependency(
				"sodium",
				"<2.0.0",
				catalog.DependencyKindIncompatible,
			)
			dependency.Unless = []catalog.DescriptorDependency{
				quiltTestDependency(
					"indium",
					"",
					catalog.DependencyKindRequired,
				),
			}
			out := []document.Package{
				quiltTestPackage(
					catalog.EcosystemQuilt,
					"consumer-project",
					quiltTestModule(
						"consumer",
						"1.0.0",
						catalog.EcosystemQuilt,
						dependency,
					),
				),
				quiltTestPackage(
					catalog.EcosystemQuilt,
					"sodium-project",
					quiltTestModule(
						"sodium",
						"1.0.0",
						catalog.EcosystemQuilt,
					),
				),
			}
			if indium {
				out = append(out, quiltTestPackage(
					catalog.EcosystemQuilt,
					"indium-project",
					quiltTestModule(
						"indium",
						"1.0.0",
						catalog.EcosystemQuilt,
					),
				))
			}
			return out
		}

		without, err := finalizeModules(packages(false), quiltTestTarget())
		if err != nil {
			t.Fatalf("finalizeModules() error = %v", err)
		}
		requireQuiltProblems(t, without, 1)

		with, err := finalizeModules(packages(true), quiltTestTarget())
		if err != nil {
			t.Fatalf("finalizeModules() error = %v", err)
		}
		requireQuiltProblems(t, with, 0)
	})

	t.Run("versioned unless does not suppress a requirement", func(t *testing.T) {
		dependency := quiltTestDependency(
			"ghost",
			"",
			catalog.DependencyKindRequired,
		)
		dependency.Unless = []catalog.DescriptorDependency{
			quiltTestDependency(
				"indium",
				">=2.0.0",
				catalog.DependencyKindRequired,
			),
		}
		packages := []document.Package{
			quiltTestPackage(
				catalog.EcosystemQuilt,
				"consumer-project",
				quiltTestModule(
					"consumer",
					"1.0.0",
					catalog.EcosystemQuilt,
					dependency,
				),
			),
			quiltTestPackage(
				catalog.EcosystemQuilt,
				"indium-project",
				quiltTestModule(
					"indium",
					"1.0.0",
					catalog.EcosystemQuilt,
				),
			),
		}
		problems, err := finalizeModules(packages, quiltTestTarget())
		if err != nil {
			t.Fatalf("finalizeModules() error = %v", err)
		}
		requireQuiltProblems(t, problems, 1)
		if problems[0].Dependency != "ghost" {
			t.Fatalf("problem = %+v", problems[0])
		}
	})
}

func TestFinalizeModulesQuiltProvidedAliasVersion(t *testing.T) {
	tests := []struct {
		name       string
		provided   map[string]versionpkg.Version
		want       int
		wantModule string
	}{
		{
			name: "provided alias carries its own version",
			provided: map[string]versionpkg.Version{
				"legacy-api": "2.0.0",
			},
			want:       0,
			wantModule: "big-mod",
		},
		{
			name: "alias version is not the provider version",
			provided: map[string]versionpkg.Version{
				"legacy-api": "1.5.0",
			},
			want: 1,
		},
		{
			name:     "unversioned alias exposes the provider version",
			provided: nil,
			want:     1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := quiltTestModule(
				"big-mod",
				"1.0.0",
				catalog.EcosystemQuilt,
			)
			provider.Provides = []string{"legacy-api"}
			provider.ProvidedVersions = tt.provided
			packages := []document.Package{
				quiltTestPackage(
					catalog.EcosystemQuilt,
					"consumer-project",
					quiltTestModule(
						"consumer",
						"1.0.0",
						catalog.EcosystemQuilt,
						quiltTestDependency(
							"legacy-api",
							">=2.0.0",
							catalog.DependencyKindRequired,
						),
					),
				),
				quiltTestPackage(
					catalog.EcosystemQuilt,
					"provider-project",
					provider,
				),
			}
			problems, err := finalizeModules(packages, quiltTestTarget())
			if err != nil {
				t.Fatalf("finalizeModules() error = %v", err)
			}
			requireQuiltProblems(t, problems, tt.want)
			if tt.want == 1 {
				wantSelected := "1.0.0"
				if tt.provided != nil {
					wantSelected = "1.5.0"
				}
				if problems[0].Dependency != "legacy-api" ||
					problems[0].Selected != wantSelected {
					t.Fatalf("problem = %+v", problems[0])
				}
				return
			}
			consumer := quiltTestModuleByID(
				t,
				packages[0].Modules,
				"consumer",
			)
			binding := consumer.Dependencies[0]
			if binding.ModuleID != tt.wantModule ||
				binding.ProjectID != "provider-project" {
				t.Fatalf("dependency binding = %+v", binding)
			}
		})
	}
}

func TestFinalizeModulesQuiltRuntimeModules(t *testing.T) {
	t.Run("quilt loader satisfies a Quilt module", func(t *testing.T) {
		packages := []document.Package{
			quiltTestPackage(
				catalog.EcosystemQuilt,
				"consumer-project",
				quiltTestModule(
					"consumer",
					"1.0.0",
					catalog.EcosystemQuilt,
					quiltTestDependency(
						"quilt_loader",
						">=0.20.0",
						catalog.DependencyKindRequired,
					),
				),
			),
		}
		problems, err := finalizeModules(packages, quiltTestTarget())
		if err != nil {
			t.Fatalf("finalizeModules() error = %v", err)
		}
		requireQuiltProblems(t, problems, 0)
	})

	t.Run("fabricloader is unresolved without a descriptor", func(t *testing.T) {
		target := quiltTestTarget()
		if provided, ok := runtimeModules(target)["fabricloader"]; ok {
			t.Fatalf(
				"runtime fabricated fabricloader %+v from the loader version",
				provided,
			)
		}

		packages := []document.Package{
			quiltTestPackage(
				catalog.EcosystemFabric,
				"fabric-project",
				quiltTestModule(
					"fabric-user",
					"1.0.0",
					catalog.EcosystemFabric,
					quiltTestDependency(
						"fabricloader",
						">=0.19.0 <0.20.0",
						catalog.DependencyKindRequired,
					),
				),
			),
		}
		problems, err := finalizeModules(packages, target)
		if err != nil {
			t.Fatalf("finalizeModules() error = %v", err)
		}
		requireQuiltProblems(t, problems, 1)
		if problems[0].Selected != "" ||
			problems[0].Server ||
			!strings.Contains(problems[0].Message, "no selected package") {
			t.Fatalf("problem = %+v", problems[0])
		}
	})

	t.Run("loader descriptor provides the fabricloader version", func(t *testing.T) {
		target := quiltTestTarget(catalog.DescriptorModule{
			ID:             "quilt_loader",
			Version:        versionpkg.Version("0.27.0"),
			Ecosystem:      catalog.EcosystemQuilt,
			VersionDialect: versionpkg.DialectFabric,
			Provides:       []string{"fabricloader"},
			ProvidedVersions: map[string]versionpkg.Version{
				"fabricloader": "0.19.5",
			},
		})
		runtime := runtimeModules(target)
		provided, ok := runtime["fabricloader"]
		if !ok || provided.Version != "0.19.5" {
			t.Fatalf("runtime fabricloader = %+v, present = %v", provided, ok)
		}
		if loader := runtime["quilt_loader"]; loader.Version != "0.27.0" {
			t.Fatalf("runtime quilt_loader = %+v", loader)
		}

		packages := []document.Package{
			quiltTestPackage(
				catalog.EcosystemFabric,
				"fabric-project",
				quiltTestModule(
					"fabric-user",
					"1.0.0",
					catalog.EcosystemFabric,
					quiltTestDependency(
						"fabricloader",
						">=0.19.0 <0.20.0",
						catalog.DependencyKindRequired,
					),
				),
			),
		}
		problems, err := finalizeModules(packages, target)
		if err != nil {
			t.Fatalf("finalizeModules() error = %v", err)
		}
		requireQuiltProblems(t, problems, 0)
	})
}
