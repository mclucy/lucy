package routing

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/mclucy/lucy/catalog"
)

type providerNamer interface {
	Provider() catalog.Provider
}

func providerKeys[P providerNamer](providers []P) []catalog.Provider {
	keys := make([]catalog.Provider, len(providers))
	for i, provider := range providers {
		keys[i] = provider.Provider()
	}
	return keys
}

func TestResolveSearchProvidersOrdering(t *testing.T) {
	tests := []struct {
		name          string
		platform      catalog.Ecosystem
		curseforgeKey string
		want          []catalog.Provider
	}{
		{
			name:     "mod loader without curseforge",
			platform: catalog.EcosystemFabric,
			want: []catalog.Provider{
				catalog.ProviderModrinth,
			},
		},
		{
			name:          "mod loader with curseforge",
			platform:      catalog.EcosystemNeoForge,
			curseforgeKey: "test-curseforge-key",
			want: []catalog.Provider{
				catalog.ProviderModrinth,
				catalog.ProviderCurseForge,
			},
		},
		{
			name:          "bukkit excludes configured curseforge",
			platform:      catalog.EcosystemBukkit,
			curseforgeKey: "test-curseforge-key",
			want: []catalog.Provider{
				catalog.ProviderModrinth,
				catalog.ProviderHangar,
				catalog.ProviderSpiget,
			},
		},
		{
			name:     "paper",
			platform: catalog.EcosystemPaper,
			want: []catalog.Provider{
				catalog.ProviderModrinth,
				catalog.ProviderHangar,
				catalog.ProviderSpiget,
			},
		},
		{
			name:     "mcdr",
			platform: catalog.EcosystemMCDR,
			want: []catalog.Provider{
				catalog.ProviderMCDR,
			},
		},
		{
			name:     "default without curseforge",
			platform: catalog.EcosystemUnspecified,
			want: []catalog.Provider{
				catalog.ProviderModrinth,
				catalog.ProviderMCDR,
			},
		},
		{
			name:          "default with curseforge",
			platform:      catalog.EcosystemUnspecified,
			curseforgeKey: "test-curseforge-key",
			want: []catalog.Provider{
				catalog.ProviderModrinth,
				catalog.ProviderCurseForge,
				catalog.ProviderMCDR,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := New(nil, nil, tt.curseforgeKey)
			providers, err := router.ResolveSearchProviders(
				tt.platform,
				catalog.RegistryUnspecified,
			)
			if err != nil {
				t.Fatalf("unexpected resolution error: %v", err)
			}
			if got := providerKeys(providers); !slices.Equal(got, tt.want) {
				t.Fatalf("got providers %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveSearchProvidersExplicitRegistry(t *testing.T) {
	router := New(nil, nil, "")

	providers, err := router.ResolveSearchProviders(
		catalog.EcosystemUnspecified,
		catalog.RegistryHangar,
	)
	if err != nil {
		t.Fatalf("unexpected explicit registry error: %v", err)
	}
	if got := providerKeys(providers); !slices.Equal(
		got,
		[]catalog.Provider{catalog.ProviderHangar},
	) {
		t.Fatalf("got explicit registry providers %v", got)
	}

	if _, err := router.ResolveSearchProviders(
		catalog.EcosystemForge,
		catalog.RegistryHangar,
	); err == nil || !strings.Contains(err.Error(), "does not support platform") {
		t.Fatalf("expected unsupported platform rejection, got %v", err)
	}

	if _, err := router.ResolveSearchProviders(
		catalog.EcosystemUnspecified,
		catalog.RegistryUnknown,
	); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("expected unknown registry error, got %v", err)
	}
}

func TestResolveSearchProvidersUnsupportedEcosystem(t *testing.T) {
	router := New(nil, nil, "")
	if _, err := router.ResolveSearchProviders(
		catalog.EcosystemSponge,
		catalog.RegistryUnspecified,
	); !errors.Is(err, ErrInvalidEcosystem) {
		t.Fatalf("expected invalid ecosystem error, got %v", err)
	}
}

func TestResolveInfoProviders(t *testing.T) {
	router := New(nil, nil, "test-curseforge-key")
	providers, err := router.ResolveInfoProviders(
		catalog.EcosystemUnspecified,
		catalog.ProviderUnspecified,
	)
	if err != nil {
		t.Fatalf("unexpected info resolution error: %v", err)
	}
	want := []catalog.Provider{
		catalog.ProviderModrinth,
		catalog.ProviderCurseForge,
		catalog.ProviderMCDR,
	}
	if got := providerKeys(providers); !slices.Equal(got, want) {
		t.Fatalf("got info providers %v, want %v", got, want)
	}

	explicit, err := router.ResolveInfoProviders(
		catalog.EcosystemFabric,
		catalog.ProviderSpiget,
	)
	if err != nil {
		t.Fatalf("unexpected explicit info provider error: %v", err)
	}
	if got := providerKeys(explicit); !slices.Equal(
		got,
		[]catalog.Provider{catalog.ProviderSpiget},
	) {
		t.Fatalf("got explicit info providers %v", got)
	}
}

func TestResolveInfoProvidersExplicitErrors(t *testing.T) {
	router := New(nil, nil, "")
	if _, err := router.ResolveInfoProviders(
		catalog.EcosystemUnspecified,
		catalog.ProviderUnknown,
	); !errors.Is(err, ErrUnknownProvider) {
		t.Fatalf("expected unknown provider error, got %v", err)
	}
	if _, err := router.ResolveInfoProviders(
		catalog.EcosystemUnspecified,
		catalog.ProviderForge,
	); !errors.Is(err, ErrUnsupportedProvider) {
		t.Fatalf("expected unsupported provider error, got %v", err)
	}
}
