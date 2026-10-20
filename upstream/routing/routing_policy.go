package routing

import (
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
)

var searchProvidersInPriorityOrder = []types.Provider{
	types.ProviderModrinth,
	types.ProviderCurseForge,
	types.ProviderHangar,
	types.ProviderSpiget,
}

func autoProviders() []types.Provider {
	return append(modProviders(), types.ProviderMCDR)
}

func modProviders() []types.Provider {
	providers := []types.Provider{types.ProviderModrinth}
	if curseforgeAvailable() {
		providers = append(providers, types.ProviderCurseForge)
	}
	return providers
}

func providersForEcosystem(ecosystem types.Ecosystem) (
	[]types.Provider,
	error,
) {
	switch ecosystem {
	case types.EcosystemUnspecified:
		return autoProviders(), nil
	case types.EcosystemMCDR:
		return []types.Provider{types.ProviderMCDR}, nil
	case types.EcosystemForge, types.EcosystemFabric, types.EcosystemNeoForge,
		types.EcosystemBukkit, types.EcosystemPaper:
		return searchProvidersForEcosystem(ecosystem), nil
	default:
		return nil, ErrInvalidEcosystem
	}
}

func searchProvidersForEcosystem(ecosystem types.Ecosystem) []types.Provider {
	providers := make(
		[]types.Provider,
		0,
		len(searchProvidersInPriorityOrder),
	)
	for _, provider := range searchProvidersInPriorityOrder {
		if provider == types.ProviderCurseForge && !curseforgeAvailable() {
			log.Debug(
				"routing: ecosystem candidate skipped",
				"provider", provider,
				"platform", ecosystem,
				"reason", "provider unavailable",
			)
			continue
		}

		support, ok := EcosystemSupportedBy(provider, ecosystem)
		if !ok || !support.Supported {
			log.Debug(
				"routing: ecosystem candidate skipped",
				"provider", provider,
				"platform", ecosystem,
				"reason", "provider does not support platform",
			)
			continue
		}

		providers = append(providers, provider)
	}
	return providers
}

type providerSelection struct {
	providers []types.Provider
	fallback  bool
	empty     bool
}

func providersFromEcosystems(ecosystems []types.Ecosystem) providerSelection {
	selection := providerSelection{}
	seen := map[types.Provider]struct{}{}
	sawKnownCapability := false
	sawProxyCapability := false

	appendProvider := func(provider types.Provider) {
		if _, ok := seen[provider]; ok {
			log.Debug(
				"routing: runtime candidate skipped",
				"provider", provider,
				"reason", "provider already selected for runtime",
			)
			return
		}
		seen[provider] = struct{}{}
		selection.providers = append(selection.providers, provider)
	}

	for _, ecosystem := range ecosystems {
		switch ecosystem {
		case types.EcosystemFabric,
			types.EcosystemForge,
			types.EcosystemNeoForge:
			sawKnownCapability = true
			appendProvider(types.ProviderModrinth)
			if curseforgeAvailable() {
				appendProvider(types.ProviderCurseForge)
			}
		case types.EcosystemBukkit,
			types.EcosystemPaper:
			sawKnownCapability = true
			appendProvider(types.ProviderModrinth)
			appendProvider(types.ProviderHangar)
			appendProvider(types.ProviderSpiget)
		case types.EcosystemMCDR:
			sawKnownCapability = true
			appendProvider(types.ProviderMCDR)
		case types.EcosystemBungeeCord, types.EcosystemVelocity:
			sawKnownCapability = true
			sawProxyCapability = true
		}
	}

	if len(selection.providers) > 0 {
		return selection
	}
	if sawProxyCapability {
		selection.empty = true
		return selection
	}
	if !sawKnownCapability {
		selection.fallback = true
	}
	selection.empty = true
	return selection
}
