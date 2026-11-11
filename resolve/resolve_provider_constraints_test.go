package resolve

import (
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	serverpkg "github.com/mclucy/lucy/server"
)

// providerConstraintPackages returns the replaced dependency at index 0, a
// Quilt provider exposing shared_id 1.0.0 at index 1, and the dependent package
// under test at index 2. Index 0 is the skipped target, as in provider
// substitution.
func providerConstraintPackages(dependent document.Package) []document.Package {
	dropped := quiltTestPackage(
		catalog.EcosystemFabric,
		"shared-id-fabric",
		quiltTestModule("shared_id", "1.0.0", catalog.EcosystemFabric),
	)
	provider := quiltTestPackage(
		catalog.EcosystemQuilt,
		"shared-id-quilt",
		quiltTestModule("shared_id", "1.0.0", catalog.EcosystemQuilt),
	)
	return []document.Package{dropped, provider, dependent}
}

func providerConstraintInEnvironment(
	pkg document.Package,
	environment serverpkg.Environment,
) document.Package {
	pkg.Environment = environment
	return pkg
}

func TestProviderSatisfiesRequiredConstraintsScopesByNamespace(t *testing.T) {
	target := quiltTestTarget()
	requiresSharedID := quiltTestDependency(
		"shared_id",
		">=2.0.0",
		catalog.DependencyKindRequired,
	)
	tests := []struct {
		name      string
		dependent document.Package
		want      bool
	}{
		{
			name: "MCDR plugin requirement is outside the provider namespace",
			dependent: providerConstraintInEnvironment(
				quiltTestPackage(
					catalog.EcosystemMCDR,
					"mcdr-plugin",
					quiltTestModule(
						"mcdr-plugin",
						"1.0.0",
						catalog.EcosystemMCDR,
						requiresSharedID,
					),
				),
				serverpkg.EnvironmentMCDR,
			),
			want: true,
		},
		{
			name: "fabric requirement in the provider namespace still blocks",
			dependent: quiltTestPackage(
				catalog.EcosystemFabric,
				"fabric-user",
				quiltTestModule(
					"fabric-user",
					"1.0.0",
					catalog.EcosystemFabric,
					requiresSharedID,
				),
			),
			want: false,
		},
		{
			name: "quilt requirement in the provider namespace still blocks",
			dependent: quiltTestPackage(
				catalog.EcosystemQuilt,
				"quilt-user",
				quiltTestModule(
					"quilt-user",
					"1.0.0",
					catalog.EcosystemQuilt,
					requiresSharedID,
				),
			),
			want: false,
		},
		{
			name: "fabric requirement in another environment is outside the provider namespace",
			dependent: providerConstraintInEnvironment(
				quiltTestPackage(
					catalog.EcosystemFabric,
					"fabric-user",
					quiltTestModule(
						"fabric-user",
						"1.0.0",
						catalog.EcosystemFabric,
						requiresSharedID,
					),
				),
				serverpkg.EnvironmentMCDR,
			),
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packages := providerConstraintPackages(tt.dependent)
			provider := packages[1]
			got := providerSatisfiesRequiredConstraints(
				packages,
				0,
				provider,
				target,
			)
			if got != tt.want {
				t.Fatalf("providerSatisfiesRequiredConstraints() = %v, want %v",
					got, tt.want)
			}
		})
	}
}
