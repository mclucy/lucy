package workspace

import (
	"testing"

	"github.com/mclucy/lucy/types"
)

func offerSet(offers []EffectiveEcosystem) map[types.Ecosystem]types.Compatibility {
	set := make(map[types.Ecosystem]types.Compatibility, len(offers))
	for _, offer := range offers {
		set[offer.Ecosystem] = offer.Compatibility
	}
	return set
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
				types.EcoPaper:  types.CompatCompatible,
				types.EcoBukkit: types.CompatCompatible,
			},
		},
		{
			name:    "youer offers neoforge paper bukkit",
			primary: compatTestCore(types.EcoUnspecified, "youer"),
			want: map[types.Ecosystem]types.Compatibility{
				types.EcoNeoforge: types.CompatCompatible,
				types.EcoPaper:    types.CompatCompatible,
				types.EcoBukkit:   types.CompatCompatible,
			},
		},
		{
			name:    "catserver offers forge and bukkit",
			primary: compatTestCore(types.EcoUnspecified, "catserver"),
			want: map[types.Ecosystem]types.Compatibility{
				types.EcoForge:  types.CompatCompatible,
				types.EcoBukkit: types.CompatCompatible,
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
				types.EcoForge:  types.CompatCompatible,
				types.EcoBukkit: types.CompatCompatible,
			},
		},
		{
			name:    "spongeforge offers sponge and forge",
			primary: compatTestCore(types.EcoSponge, "spongeforge"),
			components: []types.VersionedPackageRef{
				compatTestCore(types.EcoForge, "forge"),
			},
			want: map[types.Ecosystem]types.Compatibility{
				types.EcoSponge: types.CompatCompatible,
				types.EcoForge:  types.CompatCompatible,
			},
		},
		{
			name:    "velocity proxy",
			primary: compatTestCore(types.EcoVelocity, "velocity"),
			want: map[types.Ecosystem]types.Compatibility{
				types.EcoVelocity: types.CompatCompatible,
			},
		},
		{
			name:    "waterfall proxy",
			primary: compatTestCore(types.EcoBungeecord, "waterfall"),
			want: map[types.Ecosystem]types.Compatibility{
				types.EcoBungeecord: types.CompatCompatible,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := compatTestServer(tt.primary, tt.components...)
			got := offerSet(server.EffectiveEcosystems())
			if len(got) != len(tt.want) {
				t.Fatalf("offers = %v, want %v", got, tt.want)
			}
			for eco, compatibility := range tt.want {
				if got[eco] != compatibility {
					t.Errorf(
						"offer %s = %q, want %q",
						eco, got[eco], compatibility,
					)
				}
			}
		})
	}
}

func TestEffectiveEcosystemsConnectorBridge(t *testing.T) {
	server := compatTestServer(
		compatTestCore(types.EcoNeoforge, "neoforge"),
		compatTestCore(types.EcoNeoforge, "neoforge"),
	)
	server.Packages = []types.DiscoveredPackage{
		{Id: types.VersionedPackageRef{
			PackageRef: types.PackageRef{
				Eco:  types.EcoNeoforge,
				Name: "sinytra-connector",
			},
		}},
	}
	got := offerSet(server.EffectiveEcosystems())
	if got[types.EcoNeoforge] != types.CompatCompatible {
		t.Errorf("neoforge = %q, want compatible", got[types.EcoNeoforge])
	}
	if got[types.EcoFabric] != types.CompatDegraded {
		t.Errorf("fabric = %q, want degraded", got[types.EcoFabric])
	}
}
