package routing

import (
	"slices"
	"testing"

	"github.com/mclucy/lucy/catalog"
)

func TestProviderSourcesFromEcosystems_BukkitFamily(t *testing.T) {
	for _, curseforge := range []bool{false, true} {
		for _, ecosystem := range []catalog.Ecosystem{
			catalog.EcosystemBukkit,
			catalog.EcosystemPaper,
		} {
			t.Run(ecosystem.String(), func(t *testing.T) {
				resolution := providersFromEcosystems(
					[]catalog.Ecosystem{ecosystem},
					curseforge,
				)
				want := []catalog.Provider{
					catalog.ProviderModrinth,
					catalog.ProviderHangar,
					catalog.ProviderSpiget,
				}
				if resolution.fallback || resolution.empty {
					t.Fatalf(
						"unexpected unresolved routing: fallback=%v empty=%v",
						resolution.fallback,
						resolution.empty,
					)
				}
				if !slices.Equal(resolution.providers, want) {
					t.Errorf("got %v, want %v", resolution.providers, want)
				}
			})
		}
	}
}

func TestProviderSourcesFromEcosystems_ModLoaders(t *testing.T) {
	for _, curseforge := range []bool{false, true} {
		for _, ecosystem := range []catalog.Ecosystem{
			catalog.EcosystemFabric,
			catalog.EcosystemForge,
			catalog.EcosystemNeoForge,
		} {
			t.Run(ecosystem.String(), func(t *testing.T) {
				resolution := providersFromEcosystems(
					[]catalog.Ecosystem{ecosystem},
					curseforge,
				)
				if resolution.fallback || resolution.empty {
					t.Fatalf(
						"unexpected unresolved routing: fallback=%v empty=%v",
						resolution.fallback,
						resolution.empty,
					)
				}
				if !slices.Contains(resolution.providers, catalog.ProviderModrinth) {
					t.Errorf("expected Modrinth, got %v", resolution.providers)
				}
				hasCurseForge := slices.Contains(
					resolution.providers,
					catalog.ProviderCurseForge,
				)
				if hasCurseForge != curseforge {
					t.Errorf(
						"CurseForge availability mismatch, got sources %v",
						resolution.providers,
					)
				}
				if slices.Contains(resolution.providers, catalog.ProviderHangar) ||
					slices.Contains(resolution.providers, catalog.ProviderSpiget) {
					t.Errorf(
						"Bukkit providers must not route mod packages: %v",
						resolution.providers,
					)
				}
			})
		}
	}
}

func TestProviderSourcesFromEcosystems_MCDR(t *testing.T) {
	resolution := providersFromEcosystems(
		[]catalog.Ecosystem{catalog.EcosystemMCDR},
		false,
	)
	want := []catalog.Provider{catalog.ProviderMCDR}
	if resolution.fallback || resolution.empty ||
		!slices.Equal(resolution.providers, want) {
		t.Fatalf("got %+v, want providers %v", resolution, want)
	}
}

func TestProviderSourcesHandleUnsupportedAndProxy(t *testing.T) {
	unsupported := providersFromEcosystems(
		[]catalog.Ecosystem{catalog.EcosystemSponge},
		false,
	)
	if !unsupported.fallback || !unsupported.empty {
		t.Fatalf("unsupported ecosystem should fall back, got %+v", unsupported)
	}

	proxy := providersFromEcosystems(
		[]catalog.Ecosystem{catalog.EcosystemVelocity},
		false,
	)
	if proxy.fallback || !proxy.empty {
		t.Fatalf("known proxy ecosystem should resolve empty, got %+v", proxy)
	}
}

func TestProviderSourcesFromEcosystems_MixedRuntime(t *testing.T) {
	for _, curseforge := range []bool{false, true} {
		resolution := providersFromEcosystems(
			[]catalog.Ecosystem{catalog.EcosystemPaper, catalog.EcosystemFabric},
			curseforge,
		)
		for _, provider := range []catalog.Provider{
			catalog.ProviderModrinth,
			catalog.ProviderHangar,
			catalog.ProviderSpiget,
		} {
			if !slices.Contains(resolution.providers, provider) {
				t.Errorf("expected provider %s, got %v", provider, resolution.providers)
			}
		}
		hasCurseForge := slices.Contains(
			resolution.providers,
			catalog.ProviderCurseForge,
		)
		if hasCurseForge != curseforge {
			t.Errorf("expected CurseForge=%v, got %v", curseforge, resolution.providers)
		}
	}
}
