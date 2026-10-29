package routing

import "github.com/mclucy/lucy/catalog"

var searchProvidersInPriorityOrder = []catalog.Provider{
	catalog.ProviderModrinth,
	catalog.ProviderCurseForge,
	catalog.ProviderHangar,
	catalog.ProviderSpiget,
}

// autoProviders is the default candidate list for an unspecified platform.
// curseforge reports whether the configured provider instances include it.
func autoProviders(curseforge bool) []catalog.Provider {
	return append(modProviders(curseforge), catalog.ProviderMCDR)
}

func modProviders(curseforge bool) []catalog.Provider {
	providers := []catalog.Provider{catalog.ProviderModrinth}
	if curseforge {
		providers = append(providers, catalog.ProviderCurseForge)
	}
	return providers
}

func providersForEcosystem(
	ecosystem catalog.Ecosystem,
	curseforge bool,
) ([]catalog.Provider, error) {
	switch ecosystem {
	case catalog.EcosystemUnspecified:
		return autoProviders(curseforge), nil
	case catalog.EcosystemMCDR:
		return []catalog.Provider{catalog.ProviderMCDR}, nil
	case catalog.EcosystemForge, catalog.EcosystemFabric, catalog.EcosystemNeoForge,
		catalog.EcosystemBukkit, catalog.EcosystemPaper:
		return searchProvidersForEcosystem(ecosystem, curseforge), nil
	default:
		return nil, ErrInvalidEcosystem
	}
}

func searchProvidersForEcosystem(
	ecosystem catalog.Ecosystem,
	curseforge bool,
) []catalog.Provider {
	providers := make(
		[]catalog.Provider,
		0,
		len(searchProvidersInPriorityOrder),
	)
	for _, provider := range searchProvidersInPriorityOrder {
		if provider == catalog.ProviderCurseForge && !curseforge {
			continue
		}

		support, ok := EcosystemSupportedBy(provider, ecosystem)
		if !ok || !support.Supported {
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

func providersFromEcosystems(
	ecosystems []catalog.Ecosystem,
	curseforge bool,
) providerSelection {
	selection := providerSelection{}
	seen := map[catalog.Provider]struct{}{}
	sawKnownCapability := false
	sawProxyCapability := false

	appendProvider := func(provider catalog.Provider) {
		if _, ok := seen[provider]; ok {
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
			if curseforge {
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
