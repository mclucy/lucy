package workspace

import (
	"testing"

	"github.com/mclucy/lucy/types"
)

func offerSet(offers []EffectiveEcosystem) map[types.Ecosystem]types.CompatVerdict {
	set := make(map[types.Ecosystem]types.CompatVerdict, len(offers))
	for _, offer := range offers {
		set[offer.Ecosystem] = offer.Verdict
	}
	return set
}

func TestEffectiveEcosystems(t *testing.T) {
	tests := []struct {
		name       string
		primary    types.VersionedPackageRef
		components []types.VersionedPackageRef
		want       map[types.Ecosystem]types.CompatVerdict
	}{
		{
			name:    "vanilla offers nothing",
			primary: admissionTestCore(types.EcoMinecraft, "minecraft"),
			want:    map[types.Ecosystem]types.CompatVerdict{},
		},
		{
			name:    "purpur is a paper fork",
			primary: admissionTestCore(types.EcoUnspecified, "purpur"),
			want: map[types.Ecosystem]types.CompatVerdict{
				types.EcoPaper:  types.CompatCompatible,
				types.EcoBukkit: types.CompatCompatible,
			},
		},
		{
			name:    "youer offers neoforge paper bukkit",
			primary: admissionTestCore(types.EcoUnspecified, "youer"),
			want: map[types.Ecosystem]types.CompatVerdict{
				types.EcoNeoforge: types.CompatCompatible,
				types.EcoPaper:    types.CompatCompatible,
				types.EcoBukkit:   types.CompatCompatible,
			},
		},
		{
			name:    "catserver offers forge and bukkit",
			primary: admissionTestCore(types.EcoUnspecified, "catserver"),
			want: map[types.Ecosystem]types.CompatVerdict{
				types.EcoForge:  types.CompatCompatible,
				types.EcoBukkit: types.CompatCompatible,
			},
		},
		{
			name:    "arclight without loader component offers nothing",
			primary: admissionTestCore(types.EcoUnspecified, "arclight"),
			want:    map[types.Ecosystem]types.CompatVerdict{},
		},
		{
			name:    "arclight with forge component",
			primary: admissionTestCore(types.EcoUnspecified, "arclight"),
			components: []types.VersionedPackageRef{
				admissionTestCore(types.EcoForge, "forge"),
			},
			want: map[types.Ecosystem]types.CompatVerdict{
				types.EcoForge:  types.CompatCompatible,
				types.EcoBukkit: types.CompatCompatible,
			},
		},
		{
			name:    "spongeforge offers sponge and forge",
			primary: admissionTestCore(types.EcoSponge, "spongeforge"),
			components: []types.VersionedPackageRef{
				admissionTestCore(types.EcoForge, "forge"),
			},
			want: map[types.Ecosystem]types.CompatVerdict{
				types.EcoSponge: types.CompatCompatible,
				types.EcoForge:  types.CompatCompatible,
			},
		},
		{
			name:    "velocity proxy",
			primary: admissionTestCore(types.EcoVelocity, "velocity"),
			want: map[types.Ecosystem]types.CompatVerdict{
				types.EcoVelocity: types.CompatCompatible,
			},
		},
		{
			name:    "waterfall proxy",
			primary: admissionTestCore(types.EcoBungeecord, "waterfall"),
			want: map[types.Ecosystem]types.CompatVerdict{
				types.EcoBungeecord: types.CompatCompatible,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := admissionTestServer(tt.primary, tt.components...)
			got := offerSet(server.EffectiveEcosystems())
			if len(got) != len(tt.want) {
				t.Fatalf("offers = %v, want %v", got, tt.want)
			}
			for eco, verdict := range tt.want {
				if got[eco] != verdict {
					t.Errorf(
						"offer %s = %q, want %q",
						eco, got[eco], verdict,
					)
				}
			}
		})
	}
}

func TestEffectiveEcosystemsConnectorBridge(t *testing.T) {
	server := admissionTestServer(
		admissionTestCore(types.EcoNeoforge, "neoforge"),
		admissionTestCore(types.EcoNeoforge, "neoforge"),
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
