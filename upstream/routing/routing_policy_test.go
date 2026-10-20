package routing

import (
	"slices"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestProviderSourcesFromEcosystems_BukkitFamily(t *testing.T) {
	for _, ecosystem := range []types.Ecosystem{
		types.EcosystemBukkit,
		types.EcosystemPaper,
	} {
		t.Run(ecosystem.String(), func(t *testing.T) {
			resolution := providersFromEcosystems(
				[]types.Ecosystem{ecosystem},
			)
			want := []types.Provider{
				types.ProviderModrinth,
				types.ProviderHangar,
				types.ProviderSpiget,
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

func TestProviderSourcesFromEcosystems_ModLoaders(t *testing.T) {
	for _, ecosystem := range []types.Ecosystem{
		types.EcosystemFabric,
		types.EcosystemForge,
		types.EcosystemNeoForge,
	} {
		t.Run(ecosystem.String(), func(t *testing.T) {
			resolution := providersFromEcosystems(
				[]types.Ecosystem{ecosystem},
			)
			if resolution.fallback || resolution.empty {
				t.Fatalf(
					"unexpected unresolved routing: fallback=%v empty=%v",
					resolution.fallback,
					resolution.empty,
				)
			}
			if !slices.Contains(resolution.providers, types.ProviderModrinth) {
				t.Errorf("expected Modrinth, got %v", resolution.providers)
			}
			hasCurseForge := slices.Contains(
				resolution.providers,
				types.ProviderCurseForge,
			)
			if hasCurseForge != curseforgeAvailable() {
				t.Errorf(
					"CurseForge availability mismatch, got sources %v",
					resolution.providers,
				)
			}
			if slices.Contains(resolution.providers, types.ProviderHangar) ||
				slices.Contains(resolution.providers, types.ProviderSpiget) {
				t.Errorf(
					"Bukkit providers must not route mod packages: %v",
					resolution.providers,
				)
			}
		})
	}
}

func TestProviderSourcesFromEcosystems_MCDR(t *testing.T) {
	resolution := providersFromEcosystems(
		[]types.Ecosystem{types.EcosystemMCDR},
	)
	want := []types.Provider{types.ProviderMCDR}
	if resolution.fallback || resolution.empty ||
		!slices.Equal(resolution.providers, want) {
		t.Fatalf("got %+v, want providers %v", resolution, want)
	}
}

func TestProviderSourcesFromEcosystems_UnsupportedAndProxy(t *testing.T) {
	unsupported := providersFromEcosystems(
		[]types.Ecosystem{types.EcosystemSponge},
	)
	if !unsupported.fallback || !unsupported.empty {
		t.Fatalf("unsupported ecosystem should fall back, got %+v", unsupported)
	}

	proxy := providersFromEcosystems(
		[]types.Ecosystem{types.EcosystemVelocity},
	)
	if proxy.fallback || !proxy.empty {
		t.Fatalf("known proxy ecosystem should resolve empty, got %+v", proxy)
	}
}

func TestProviderSourcesFromEcosystems_MixedRuntime(t *testing.T) {
	resolution := providersFromEcosystems(
		[]types.Ecosystem{types.EcosystemPaper, types.EcosystemFabric},
	)
	for _, provider := range []types.Provider{
		types.ProviderModrinth,
		types.ProviderHangar,
		types.ProviderSpiget,
	} {
		if !slices.Contains(resolution.providers, provider) {
			t.Errorf("expected provider %s, got %v", provider, resolution.providers)
		}
	}
	if curseforgeAvailable() &&
		!slices.Contains(resolution.providers, types.ProviderCurseForge) {
		t.Errorf("expected CurseForge when available, got %v", resolution.providers)
	}
}
