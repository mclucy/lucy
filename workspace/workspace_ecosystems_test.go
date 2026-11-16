package workspace

import (
	"testing"

	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace/discovery"
)

// compatWorkspace builds an observation whose discovery selected exactly one
// runtime, as the scanner would have produced it.
func compatWorkspace(
	primary catalog.VersionedRef,
	components ...catalog.VersionedRef,
) *Workspace {
	installations := []discovery.Installation{
		discovery.NewInstallation(
			discovery.Identity{Primary: primary, Components: components},
			"/server.jar",
		),
	}
	selection := discovery.Selection{
		State:        discovery.SelectionSelected,
		Source:       discovery.SourceInferred,
		Installation: &installations[0],
	}
	return &Workspace{
		Discovery: discovery.Result{
			Installations: installations,
			Selection:     selection,
		},
	}
}

func compatTestCore(
	ecosystem catalog.Ecosystem,
	name string,
) catalog.VersionedRef {
	return catalog.VersionedRef{
		Ecosystem: ecosystem,
		Name:      catalog.Name(name),
		Version:   "1.0.0",
	}
}

func offerSet(offers []ServedEcosystem) map[catalog.Ecosystem]Compatibility {
	set := make(map[catalog.Ecosystem]Compatibility, len(offers))
	for _, offer := range offers {
		set[offer.Ecosystem] = offer.Compatibility
	}
	return set
}

func TestServedEcosystems(t *testing.T) {
	tests := []struct {
		name       string
		primary    catalog.VersionedRef
		components []catalog.VersionedRef
		packages   []DiscoveredPackage
		want       map[catalog.Ecosystem]Compatibility
	}{
		{
			name:    "vanilla offers nothing",
			primary: compatTestCore(catalog.EcosystemMinecraft, "minecraft"),
			want:    map[catalog.Ecosystem]Compatibility{},
		},
		{
			name:    "fabric runtime offers fabric",
			primary: compatTestCore(catalog.EcosystemFabric, "fabric"),
			want: map[catalog.Ecosystem]Compatibility{
				catalog.EcosystemFabric: CompatFull,
			},
		},
		{
			name:    "connector degrades fabric on neoforge",
			primary: compatTestCore(catalog.EcosystemNeoForge, "neoforge"),
			packages: []DiscoveredPackage{
				{
					ID: compatTestCore(
						catalog.EcosystemNeoForge,
						"sinytra-connector",
					),
				},
			},
			want: map[catalog.Ecosystem]Compatibility{
				catalog.EcosystemNeoForge: CompatFull,
				catalog.EcosystemFabric:   CompatDegraded,
			},
		},
		{
			name:    "purpur is a paper fork",
			primary: compatTestCore(catalog.EcosystemUnspecified, "purpur"),
			want: map[catalog.Ecosystem]Compatibility{
				catalog.EcosystemPaper:  CompatFull,
				catalog.EcosystemBukkit: CompatFull,
			},
		},
		{
			name:    "youer offers neoforge paper bukkit",
			primary: compatTestCore(catalog.EcosystemUnspecified, "youer"),
			want: map[catalog.Ecosystem]Compatibility{
				catalog.EcosystemNeoForge: CompatFull,
				catalog.EcosystemPaper:    CompatFull,
				catalog.EcosystemBukkit:   CompatFull,
			},
		},
		{
			name:    "catserver offers forge and bukkit",
			primary: compatTestCore(catalog.EcosystemUnspecified, "catserver"),
			want: map[catalog.Ecosystem]Compatibility{
				catalog.EcosystemForge:  CompatFull,
				catalog.EcosystemBukkit: CompatFull,
			},
		},
		{
			name:    "arclight without loader component offers nothing",
			primary: compatTestCore(catalog.EcosystemUnspecified, "arclight"),
			want:    map[catalog.Ecosystem]Compatibility{},
		},
		{
			name:    "arclight with forge component",
			primary: compatTestCore(catalog.EcosystemUnspecified, "arclight"),
			components: []catalog.VersionedRef{
				compatTestCore(catalog.EcosystemForge, "forge"),
			},
			want: map[catalog.Ecosystem]Compatibility{
				catalog.EcosystemForge:  CompatFull,
				catalog.EcosystemBukkit: CompatFull,
			},
		},
		{
			name:    "arclight with neoforge component",
			primary: compatTestCore(catalog.EcosystemUnspecified, "arclight"),
			components: []catalog.VersionedRef{
				compatTestCore(catalog.EcosystemNeoForge, "neoforge"),
			},
			want: map[catalog.Ecosystem]Compatibility{
				catalog.EcosystemNeoForge: CompatFull,
				catalog.EcosystemBukkit:   CompatFull,
			},
		},
		{
			name:    "spongeforge offers sponge and forge",
			primary: compatTestCore(catalog.EcosystemSponge, "spongeforge"),
			components: []catalog.VersionedRef{
				compatTestCore(catalog.EcosystemForge, "forge"),
			},
			want: map[catalog.Ecosystem]Compatibility{
				catalog.EcosystemSponge: CompatFull,
				catalog.EcosystemForge:  CompatFull,
			},
		},
		{
			name:    "velocity proxy",
			primary: compatTestCore(catalog.EcosystemVelocity, "velocity"),
			want: map[catalog.Ecosystem]Compatibility{
				catalog.EcosystemVelocity: CompatFull,
			},
		},
		{
			name:    "waterfall proxy",
			primary: compatTestCore(catalog.EcosystemBungeeCord, "waterfall"),
			want: map[catalog.Ecosystem]Compatibility{
				catalog.EcosystemBungeeCord: CompatFull,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ws := compatWorkspace(tt.primary, tt.components...)
			ws.Packages = tt.packages
			got := offerSet(ws.ServedEcosystems())
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
	platform catalog.Ecosystem,
	name, version, path string,
) DiscoveredPackage {
	t.Helper()
	return DiscoveredPackage{
		ID: catalog.VersionedRef{
			Ref: catalog.Ref{
				Name:     catalog.Name(name),
				Provider: catalog.ProviderUnknown,
			},
			Ecosystem: platform,
			Version:   versionpkg.Version(version),
		},
		Path: path,
	}
}
