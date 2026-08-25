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
			Candidates: []RuntimeCandidate{{
				JarPath: "server.jar",
				Evidence: &detector.ExecutableEvidence{
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
		PackageRef: types.PackageRef{
			Eco:  ecosystem,
			Name: types.BarePackageName(name),
		},
		Version: "1.0.0",
	}
}

func offerSet(offers []EffectiveEcosystem) map[types.Ecosystem]types.Compatibility {
	set := make(map[types.Ecosystem]types.Compatibility, len(offers))
	for _, offer := range offers {
		set[offer.Ecosystem] = offer.Compatibility
	}
	return set
}

func TestSupports_UnobservedRuntime(t *testing.T) {
	for _, ws := range []Workspace{{}, compatWorkspace(types.VersionedPackageRef{})} {
		offered, level, ok := ws.Supports(
			compatTestCore(types.EcoFabric, "lithium"),
		)
		if ok || offered != types.EcoUnspecified {
			t.Errorf(
				"expected no offer from an empty observation, got %q/%q/%v",
				offered,
				level,
				ok,
			)
		}
	}
}

func TestSupports_FullAndMissing(t *testing.T) {
	ws := compatWorkspace(compatTestCore(types.EcoFabric, "fabric"))

	offered, level, ok := ws.Supports(
		compatTestCore(types.EcoFabric, "lithium"),
	)
	if !ok || level != types.CompatFull || offered != types.EcoFabric {
		t.Errorf(
			"expected full Fabric support, got %q/%q/%v",
			offered,
			level,
			ok,
		)
	}

	if _, _, ok := ws.Supports(
		compatTestCore(types.EcoForge, "luckperms"),
	); ok {
		t.Errorf("expected no Forge support on a Fabric runtime")
	}
}

func TestSupports_ConnectorIsDegraded(t *testing.T) {
	ws := compatWorkspace(compatTestCore(types.EcoNeoforge, "neoforge"))
	ws.Packages = []types.DiscoveredPackage{
		{
			Id: compatTestCore(
				types.EcoNeoforge,
				"sinytra-connector",
			),
		},
	}

	offered, level, ok := ws.Supports(
		compatTestCore(types.EcoFabric, "lithium"),
	)
	if !ok || level != types.CompatDegraded || offered != types.EcoFabric {
		t.Fatalf(
			"expected degraded Fabric support through the bridge, got %q/%q/%v",
			offered,
			level,
			ok,
		)
	}
}

func TestSupports_PaperSatisfiesBukkit(t *testing.T) {
	ws := compatWorkspace(compatTestCore(types.EcoPaper, "paper"))
	_, level, ok := ws.Supports(compatTestCore(types.EcoBukkit, "essentials"))
	if !ok || level != types.CompatFull {
		t.Errorf("expected full Bukkit support on Paper, got %q/%v", level, ok)
	}
}

func TestSupports_HybridCoreOffersBothEcosystems(t *testing.T) {
	ws := compatWorkspace(
		compatTestCore(types.EcoUnspecified, "arclight"),
		compatTestCore(types.EcoNeoforge, "neoforge"),
	)
	for _, ecosystem := range []types.Ecosystem{
		types.EcoNeoforge,
		types.EcoBukkit,
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

func TestEffectiveEcosystems(t *testing.T) {
	tests := []struct {
		name       string
		primary    types.VersionedPackageRef
		components []types.VersionedPackageRef
		want       map[types.Ecosystem]types.Compatibility
	}{
		{
			name:    "vanilla offers nothing",
			primary: compatTestCore(types.EcoMinecraft, "minecraft"),
			want:    map[types.Ecosystem]types.Compatibility{},
		},
		{
			name:    "purpur is a paper fork",
			primary: compatTestCore(types.EcoUnspecified, "purpur"),
			want: map[types.Ecosystem]types.Compatibility{
				types.EcoPaper:  types.CompatFull,
				types.EcoBukkit: types.CompatFull,
			},
		},
		{
			name:    "youer offers neoforge paper bukkit",
			primary: compatTestCore(types.EcoUnspecified, "youer"),
			want: map[types.Ecosystem]types.Compatibility{
				types.EcoNeoforge: types.CompatFull,
				types.EcoPaper:    types.CompatFull,
				types.EcoBukkit:   types.CompatFull,
			},
		},
		{
			name:    "catserver offers forge and bukkit",
			primary: compatTestCore(types.EcoUnspecified, "catserver"),
			want: map[types.Ecosystem]types.Compatibility{
				types.EcoForge:  types.CompatFull,
				types.EcoBukkit: types.CompatFull,
			},
		},
		{
			name:    "arclight without loader component offers nothing",
			primary: compatTestCore(types.EcoUnspecified, "arclight"),
			want:    map[types.Ecosystem]types.Compatibility{},
		},
		{
			name:    "arclight with forge component",
			primary: compatTestCore(types.EcoUnspecified, "arclight"),
			components: []types.VersionedPackageRef{
				compatTestCore(types.EcoForge, "forge"),
			},
			want: map[types.Ecosystem]types.Compatibility{
				types.EcoForge:  types.CompatFull,
				types.EcoBukkit: types.CompatFull,
			},
		},
		{
			name:    "spongeforge offers sponge and forge",
			primary: compatTestCore(types.EcoSponge, "spongeforge"),
			components: []types.VersionedPackageRef{
				compatTestCore(types.EcoForge, "forge"),
			},
			want: map[types.Ecosystem]types.Compatibility{
				types.EcoSponge: types.CompatFull,
				types.EcoForge:  types.CompatFull,
			},
		},
		{
			name:    "velocity proxy",
			primary: compatTestCore(types.EcoVelocity, "velocity"),
			want: map[types.Ecosystem]types.Compatibility{
				types.EcoVelocity: types.CompatFull,
			},
		},
		{
			name:    "waterfall proxy",
			primary: compatTestCore(types.EcoBungeecord, "waterfall"),
			want: map[types.Ecosystem]types.Compatibility{
				types.EcoBungeecord: types.CompatFull,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := buildServerInstance(&detector.ExecutableEvidence{
				PrimaryRuntime:    &tt.primary,
				PrimaryPath:       "server.jar",
				RuntimeComponents: tt.components,
			})
			got := offerSet(server.effectiveEcosystems())
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

func TestEffectiveEcosystemsConnectorBridge(t *testing.T) {
	ws := compatWorkspace(
		compatTestCore(types.EcoNeoforge, "neoforge"),
		compatTestCore(types.EcoNeoforge, "neoforge"),
	)
	ws.Packages = []types.DiscoveredPackage{
		{
			Id: compatTestCore(types.EcoNeoforge, "sinytra-connector"),
		},
	}
	got := offerSet(ws.EffectiveEcosystems())
	if got[types.EcoNeoforge] != types.CompatFull {
		t.Errorf("neoforge = %q, want compatible", got[types.EcoNeoforge])
	}
	if got[types.EcoFabric] != types.CompatDegraded {
		t.Errorf("fabric = %q, want degraded", got[types.EcoFabric])
	}
}

func makeDiscoveredPackage(
	t *testing.T,
	platform types.Ecosystem,
	name, version, path string,
) types.DiscoveredPackage {
	t.Helper()
	return types.DiscoveredPackage{
		Id: types.VersionedPackageRef{
			PackageRef: types.PackageRef{
				Eco:  platform,
				Name: types.BarePackageName(name),
			},
			Version: types.BareVersion(version),
		},
		Path: path,
	}
}
