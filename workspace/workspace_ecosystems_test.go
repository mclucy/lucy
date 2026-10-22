package workspace

import (
	"testing"

	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace/internal/detector"
)

// compatWorkspace builds an observation whose probe names exactly one
// runtime, as a scanner would have produced it.
func compatWorkspace(
	primary catalog.VersionedRef,
	components ...catalog.VersionedRef,
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

func TestSupports_FullAndMissing(t *testing.T) {
	ws := compatWorkspace(compatTestCore(catalog.EcosystemFabric, "fabric"))

	offered, level, ok := ws.Supports(
		compatTestCore(catalog.EcosystemFabric, "lithium"),
	)
	if !ok || level != CompatFull || offered != catalog.EcosystemFabric {
		t.Errorf(
			"expected full Fabric support, got %q/%q/%v",
			offered,
			level,
			ok,
		)
	}

	if _, _, ok := ws.Supports(
		compatTestCore(catalog.EcosystemForge, "luckperms"),
	); ok {
		t.Errorf("expected no Forge support on a Fabric runtime")
	}
}

func TestSupports_ConnectorIsDegraded(t *testing.T) {
	ws := compatWorkspace(compatTestCore(catalog.EcosystemNeoForge, "neoforge"))
	ws.Packages = []DiscoveredPackage{
		{
			ID: compatTestCore(
				catalog.EcosystemNeoForge,
				"sinytra-connector",
			),
		},
	}

	offered, level, ok := ws.Supports(
		compatTestCore(catalog.EcosystemFabric, "lithium"),
	)
	if !ok || level != CompatDegraded || offered != catalog.EcosystemFabric {
		t.Fatalf(
			"expected degraded Fabric support through the bridge, got %q/%q/%v",
			offered,
			level,
			ok,
		)
	}
}

func TestSupports_PaperSatisfiesBukkit(t *testing.T) {
	ws := compatWorkspace(compatTestCore(catalog.EcosystemPaper, "paper"))
	_, level, ok := ws.Supports(compatTestCore(catalog.EcosystemBukkit, "essentials"))
	if !ok || level != CompatFull {
		t.Errorf("expected full Bukkit support on Paper, got %q/%v", level, ok)
	}
}

func TestSupports_HybridCoreOffersBothEcosystems(t *testing.T) {
	ws := compatWorkspace(
		compatTestCore(catalog.EcosystemUnspecified, "arclight"),
		compatTestCore(catalog.EcosystemNeoForge, "neoforge"),
	)
	for _, ecosystem := range []catalog.Ecosystem{
		catalog.EcosystemNeoForge,
		catalog.EcosystemBukkit,
	} {
		_, level, ok := ws.Supports(compatTestCore(ecosystem, "pkg"))
		if !ok || level != CompatFull {
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
		primary    catalog.VersionedRef
		components []catalog.VersionedRef
		want       map[catalog.Ecosystem]Compatibility
	}{
		{
			name:    "vanilla offers nothing",
			primary: compatTestCore(catalog.EcosystemMinecraft, "minecraft"),
			want:    map[catalog.Ecosystem]Compatibility{},
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
