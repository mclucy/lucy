package routing

import (
	"slices"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestProviderSourcesFromEcosystems_BukkitFamily(t *testing.T) {
	for _, ecosystem := range []types.Ecosystem{
		types.EcoBukkit,
		types.EcoPaper,
	} {
		t.Run(ecosystem.String(), func(t *testing.T) {
			resolution := providerSourcesFromEcosystems(
				[]types.Ecosystem{ecosystem},
			)
			want := []types.SourceId{
				types.SourceModrinth,
				types.SourceHangar,
				types.SourceSpiget,
			}
			if resolution.fallback || resolution.empty {
				t.Fatalf(
					"unexpected unresolved routing: fallback=%v empty=%v",
					resolution.fallback,
					resolution.empty,
				)
			}
			if !slices.Equal(resolution.sources, want) {
				t.Errorf("got %v, want %v", resolution.sources, want)
			}
		})
	}
}

func TestProviderSourcesFromEcosystems_ModLoaders(t *testing.T) {
	for _, ecosystem := range []types.Ecosystem{
		types.EcoFabric,
		types.EcoForge,
		types.EcoNeoforge,
	} {
		t.Run(ecosystem.String(), func(t *testing.T) {
			resolution := providerSourcesFromEcosystems(
				[]types.Ecosystem{ecosystem},
			)
			if resolution.fallback || resolution.empty {
				t.Fatalf(
					"unexpected unresolved routing: fallback=%v empty=%v",
					resolution.fallback,
					resolution.empty,
				)
			}
			if !slices.Contains(resolution.sources, types.SourceModrinth) {
				t.Errorf("expected Modrinth, got %v", resolution.sources)
			}
			hasCurseForge := slices.Contains(
				resolution.sources,
				types.SourceCurseForge,
			)
			if hasCurseForge != DefaultRegistry().has(types.SourceCurseForge) {
				t.Errorf(
					"CurseForge availability mismatch, got sources %v",
					resolution.sources,
				)
			}
			if slices.Contains(resolution.sources, types.SourceHangar) ||
				slices.Contains(resolution.sources, types.SourceSpiget) {
				t.Errorf(
					"Bukkit sources must not route mod packages: %v",
					resolution.sources,
				)
			}
		})
	}
}

func TestProviderSourcesFromEcosystems_MCDR(t *testing.T) {
	resolution := providerSourcesFromEcosystems(
		[]types.Ecosystem{types.EcoMcdr},
	)
	want := []types.SourceId{types.SourceMCDR}
	if resolution.fallback || resolution.empty ||
		!slices.Equal(resolution.sources, want) {
		t.Fatalf("got %+v, want sources %v", resolution, want)
	}
}

func TestProviderSourcesFromEcosystems_UnsupportedAndProxy(t *testing.T) {
	unsupported := providerSourcesFromEcosystems(
		[]types.Ecosystem{types.EcoSponge},
	)
	if !unsupported.fallback || !unsupported.empty {
		t.Fatalf("unsupported ecosystem should fall back, got %+v", unsupported)
	}

	proxy := providerSourcesFromEcosystems(
		[]types.Ecosystem{types.EcoVelocity},
	)
	if proxy.fallback || !proxy.empty {
		t.Fatalf("known proxy ecosystem should resolve empty, got %+v", proxy)
	}
}

func TestProviderSourcesFromEcosystems_MixedRuntime(t *testing.T) {
	resolution := providerSourcesFromEcosystems(
		[]types.Ecosystem{types.EcoPaper, types.EcoFabric},
	)
	for _, source := range []types.SourceId{
		types.SourceModrinth,
		types.SourceHangar,
		types.SourceSpiget,
	} {
		if !slices.Contains(resolution.sources, source) {
			t.Errorf("expected source %s, got %v", source, resolution.sources)
		}
	}
	if DefaultRegistry().has(types.SourceCurseForge) &&
		!slices.Contains(resolution.sources, types.SourceCurseForge) {
		t.Errorf("expected CurseForge when available, got %v", resolution.sources)
	}
}
