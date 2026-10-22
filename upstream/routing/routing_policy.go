package routing

import (
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/log"
)

var searchProvidersInPriorityOrder = []catalog.Provider{
	catalog.ProviderModrinth,
	catalog.ProviderCurseForge,
	catalog.ProviderHangar,
	catalog.ProviderSpiget,
}

func autoProviders() []catalog.Provider {
	return append(modProviders(), catalog.ProviderMCDR)
}

func modProviders() []catalog.Provider {
	providers := []catalog.Provider{catalog.ProviderModrinth}
	if curseforgeAvailable() {
		providers = append(providers, catalog.ProviderCurseForge)
	}
	return providers
}

func providersForEcosystem(ecosystem catalog.Ecosystem) (
	[]catalog.Provider,
	error,
) {
	switch ecosystem {
	case catalog.EcosystemUnspecified:
		return autoProviders(), nil
	case catalog.EcosystemMCDR:
		return []catalog.Provider{catalog.ProviderMCDR}, nil
	case catalog.EcosystemForge, catalog.EcosystemFabric, catalog.EcosystemNeoForge,
		catalog.EcosystemBukkit, catalog.EcosystemPaper:
		return searchProvidersForEcosystem(ecosystem), nil
	default:
		return nil, ErrInvalidEcosystem
	}
}

func searchProvidersForEcosystem(ecosystem catalog.Ecosystem) []catalog.Provider {
	providers := make(
		[]catalog.Provider,
		0,
		len(searchProvidersInPriorityOrder),
	)
	for _, provider := range searchProvidersInPriorityOrder {
		if provider == catalog.ProviderCurseForge && !curseforgeAvailable() {
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
	providers []catalog.Provider
	fallback  bool
	empty     bool
}

func providersFromEcosystems(ecosystems []catalog.Ecosystem) providerSelection {
	selection := providerSelection{}
	seen := map[catalog.Provider]struct{}{}
	sawKnownCapability := false
	sawProxyCapability := false

	appendProvider := func(provider catalog.Provider) {
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
		case catalog.EcosystemFabric,
			catalog.EcosystemForge,
			catalog.EcosystemNeoForge:
			sawKnownCapability = true
			appendProvider(catalog.ProviderModrinth)
			if curseforgeAvailable() {
				appendProvider(catalog.ProviderCurseForge)
			}
		case catalog.EcosystemBukkit,
			catalog.EcosystemPaper:
			sawKnownCapability = true
			appendProvider(catalog.ProviderModrinth)
			appendProvider(catalog.ProviderHangar)
			appendProvider(catalog.ProviderSpiget)
		case catalog.EcosystemMCDR:
			sawKnownCapability = true
			appendProvider(catalog.ProviderMCDR)
		case catalog.EcosystemBungeeCord, catalog.EcosystemVelocity:
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
