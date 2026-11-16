package resolve

import (
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	serverpkg "github.com/mclucy/lucy/server"
)

// quiltClientOnlyPackages declares shared_id as client-only on a Quilt server
// package, then adds one dependent module that requires shared_id from the
// given environment and ecosystem.
func quiltClientOnlyPackages(
	dependentEnvironment serverpkg.Environment,
	dependentEcosystem catalog.Ecosystem,
) []document.Package {
	declared := quiltTestModule(
		"quilt_shared_provider",
		"1.0.0",
		catalog.EcosystemQuilt,
	)
	declared.ClientOnly = []string{"shared_id"}

	dependent := quiltTestPackage(
		dependentEcosystem,
		"dependent-project",
		quiltTestModule(
			"dependent",
			"1.0.0",
			dependentEcosystem,
			quiltTestDependency(
				"shared_id",
				"",
				catalog.DependencyKindRequired,
			),
		),
	)
	dependent.Environment = dependentEnvironment

	return []document.Package{
		quiltTestPackage(
			catalog.EcosystemQuilt,
			"quilt-project",
			declared,
		),
		dependent,
	}
}

// TestFinalizeModulesClientOnlyIsScopedByEnvironmentAndEcosystem checks that a
// client-only identity waives only requirements in its own environment and
// ecosystem namespace. A Quilt-declared client-only identity must not hide a
// missing MCDR plugin requirement that happens to share the identifier.
func TestFinalizeModulesClientOnlyIsScopedByEnvironmentAndEcosystem(t *testing.T) {
	tests := []struct {
		name        string
		environment serverpkg.Environment
		ecosystem   catalog.Ecosystem
		want        int
	}{
		{
			name:        "mcdr plugin is not waived by a quilt client-only identity",
			environment: serverpkg.EnvironmentMCDR,
			ecosystem:   catalog.EcosystemMCDR,
			want:        1,
		},
		{
			name:        "mcdr ecosystem in server environment is not waived",
			environment: serverpkg.EnvironmentServer,
			ecosystem:   catalog.EcosystemMCDR,
			want:        1,
		},
		{
			name:        "quilt dependent in server environment is waived",
			environment: serverpkg.EnvironmentServer,
			ecosystem:   catalog.EcosystemQuilt,
			want:        0,
		},
		{
			name:        "fabric dependent in server environment is waived",
			environment: serverpkg.EnvironmentServer,
			ecosystem:   catalog.EcosystemFabric,
			want:        0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := quiltTestTarget()
			target.MCDRVersion = "2.4.0"

			problems, err := finalizeModules(
				quiltClientOnlyPackages(tt.environment, tt.ecosystem),
				target,
			)
			if err != nil {
				t.Fatalf("finalizeModules() error = %v", err)
			}
			if len(problems) != tt.want {
				t.Fatalf("problems = %+v, want %d", problems, tt.want)
			}
			if tt.want == 1 &&
				(problems[0].Dependency != "shared_id" ||
					problems[0].RequiredBy != "dependent") {
				t.Fatalf("problem = %+v", problems[0])
			}
		})
	}
}
