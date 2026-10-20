package workspace

import (
	"testing"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace/internal/detector"
)

// compatWorkspace builds an observation whose probe names exactly one
// runtime, as a scanner would have produced it.
func compatWorkspace(
	primary types.VersionedPackageRef,
	components ...types.VersionedPackageRef,
) Workspace {
	return Workspace{
		Probe: Probe{
			Candidates: []IdentifiedRuntime{{
				JarPath: "server.jar",
				Evidence: &detector.RuntimeEvidence{
					PrimaryRuntime:    &primary,
					PrimaryPath:       "server.jar",
					RuntimeComponents: components,
				},
			}},
		},
	}
}

func compatTestCore(
	ecosystem types.Ecosystem,
	name string,
) types.VersionedPackageRef {
	return types.VersionedPackageRef{
		Ecosystem: ecosystem,
		Name:      types.PackageName(name),
		Version:   "1.0.0",
	}
}

func offerSet(offers []ServedEcosystem) map[types.Ecosystem]types.Compatibility {
	set := make(map[types.Ecosystem]types.Compatibility, len(offers))
	for _, offer := range offers {
		set[offer.Ecosystem] = offer.Compatibility
	}
	return set
}

func TestSupports_FullAndMissing(t *testing.T) {
	ws := compatWorkspace(compatTestCore(types.EcosystemFabric, "fabric"))

	offered, level, ok := ws.Supports(
		compatTestCore(types.EcosystemFabric, "lithium"),
	)
	if !ok || level != types.CompatFull || offered != types.EcosystemFabric {
		t.Errorf(
			"expected full Fabric support, got %q/%q/%v",
			offered,
			level,
			ok,
		)
	}

	if _, _, ok := ws.Supports(
		compatTestCore(types.EcosystemForge, "luckperms"),
	); ok {
		t.Errorf("expected no Forge support on a Fabric runtime")
	}
}

func TestSupports_ConnectorIsDegraded(t *testing.T) {
	ws := compatWorkspace(compatTestCore(types.EcosystemNeoForge, "neoforge"))
	ws.Packages = []types.DiscoveredPackage{
		{
			ID: compatTestCore(
				types.EcosystemNeoForge,
				"sinytra-connector",
			),
		},
	}

	offered, level, ok := ws.Supports(
		compatTestCore(types.EcosystemFabric, "lithium"),
	)
	if !ok || level != types.CompatDegraded || offered != types.EcosystemFabric {
		t.Fatalf(
			"expected degraded Fabric support through the bridge, got %q/%q/%v",
			offered,
			level,
			ok,
		)
	}
}

func TestSupports_PaperSatisfiesBukkit(t *testing.T) {
	ws := compatWorkspace(compatTestCore(types.EcosystemPaper, "paper"))
	_, level, ok := ws.Supports(compatTestCore(types.EcosystemBukkit, "essentials"))
	if !ok || level != types.CompatFull {
		t.Errorf("expected full Bukkit support on Paper, got %q/%v", level, ok)
	}
}

func TestSupports_HybridCoreOffersBothEcosystems(t *testing.T) {
	ws := compatWorkspace(
		compatTestCore(types.EcosystemUnspecified, "arclight"),
		compatTestCore(types.EcosystemNeoForge, "neoforge"),
	)
	for _, ecosystem := range []types.Ecosystem{
		types.EcosystemNeoForge,
		types.EcosystemBukkit,
	} {
		_, level, ok := ws.Supports(compatTestCore(ecosystem, "pkg"))
		if !ok || level != types.CompatFull {
			t.Errorf(
				"expected full %s support, got %q/%v",
				ecosystem,
				level,
				ok,
			)
		}
	}
}

func TestServedEcosystems(t *testing.T) {
	tests := []struct {
		name       string
		primary    types.VersionedPackageRef
		components []types.VersionedPackageRef
		want       map[types.Ecosystem]types.Compatibility
	}{
		{
			name:    "vanilla offers nothing",
			primary: compatTestCore(types.EcosystemMinecraft, "minecraft"),
			want:    map[types.Ecosystem]types.Compatibility{},
		},
		{
			name:    "purpur is a paper fork",
			primary: compatTestCore(types.EcosystemUnspecified, "purpur"),
			want: map[types.Ecosystem]types.Compatibility{
				types.EcosystemPaper:  types.CompatFull,
				types.EcosystemBukkit: types.CompatFull,
			},
		},
		{
			name:    "youer offers neoforge paper bukkit",
			primary: compatTestCore(types.EcosystemUnspecified, "youer"),
			want: map[types.Ecosystem]types.Compatibility{
				types.EcosystemNeoForge: types.CompatFull,
				types.EcosystemPaper:    types.CompatFull,
				types.EcosystemBukkit:   types.CompatFull,
			},
		},
		{
			name:    "catserver offers forge and bukkit",
			primary: compatTestCore(types.EcosystemUnspecified, "catserver"),
			want: map[types.Ecosystem]types.Compatibility{
				types.EcosystemForge:  types.CompatFull,
				types.EcosystemBukkit: types.CompatFull,
			},
		},
		{
			name:    "arclight without loader component offers nothing",
			primary: compatTestCore(types.EcosystemUnspecified, "arclight"),
			want:    map[types.Ecosystem]types.Compatibility{},
		},
		{
			name:    "arclight with forge component",
			primary: compatTestCore(types.EcosystemUnspecified, "arclight"),
			components: []types.VersionedPackageRef{
				compatTestCore(types.EcosystemForge, "forge"),
			},
			want: map[types.Ecosystem]types.Compatibility{
				types.EcosystemForge:  types.CompatFull,
				types.EcosystemBukkit: types.CompatFull,
			},
		},
		{
			name:    "spongeforge offers sponge and forge",
			primary: compatTestCore(types.EcosystemSponge, "spongeforge"),
			components: []types.VersionedPackageRef{
				compatTestCore(types.EcosystemForge, "forge"),
			},
			want: map[types.Ecosystem]types.Compatibility{
				types.EcosystemSponge: types.CompatFull,
				types.EcosystemForge:  types.CompatFull,
			},
		},
		{
			name:    "velocity proxy",
			primary: compatTestCore(types.EcosystemVelocity, "velocity"),
			want: map[types.Ecosystem]types.Compatibility{
				types.EcosystemVelocity: types.CompatFull,
			},
		},
		{
			name:    "waterfall proxy",
			primary: compatTestCore(types.EcosystemBungeeCord, "waterfall"),
			want: map[types.Ecosystem]types.Compatibility{
				types.EcosystemBungeeCord: types.CompatFull,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := buildServerRuntime(&detector.RuntimeEvidence{
				PrimaryRuntime:    &tt.primary,
				PrimaryPath:       "server.jar",
				RuntimeComponents: tt.components,
			})
			got := offerSet(server.servedEcosystems())
			if len(got) != len(tt.want) {
				t.Fatalf(
					"offers = %v, want %v",
					got,
					tt.want,
				)
			}
			for ecosystem, compatibility := range tt.want {
				if got[ecosystem] != compatibility {
					t.Errorf(
						"offer %s = %q, want %q",
						ecosystem,
						got[ecosystem],
						compatibility,
					)
				}
			}
		})
	}
}

func makeDiscoveredPackage(
	t *testing.T,
	platform types.Ecosystem,
	name, version, path string,
) types.DiscoveredPackage {
	t.Helper()
	return types.DiscoveredPackage{
		ID: types.VersionedPackageRef{
			PackageRef: types.PackageRef{
				Name:     types.PackageName(name),
				Provider: types.ProviderUnknown,
			},
			Ecosystem: platform,
			Version:   types.Version(version),
		},
		Path: path,
	}
}
