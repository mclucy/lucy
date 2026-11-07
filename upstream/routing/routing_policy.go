package routing

import "github.com/mclucy/lucy/catalog"

// providersInPriorityOrder is the provider preference order shared by
// ecosystem selection and search selection.
var providersInPriorityOrder = []catalog.Provider{
	catalog.ProviderModrinth,
	catalog.ProviderCurseForge,
	catalog.ProviderHangar,
	catalog.ProviderSpiget,
	catalog.ProviderMCDR,
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

// providersForEcosystem returns the ordered providers that serve ecosystem.
// The result is filtered by configured credentials, so a supported ecosystem
// may still have no providers.
func providersForEcosystem(
	ecosystem catalog.Ecosystem,
	curseforge bool,
) []catalog.Provider {
	if ecosystem == catalog.EcosystemUnspecified {
		return autoProviders(curseforge)
	}

	providers := make(
		[]catalog.Provider,
		0,
		len(providersInPriorityOrder),
	)
	for _, provider := range providersInPriorityOrder {
		if !EcosystemSupportedBy(provider, ecosystem) {
			continue
		}
		if provider == catalog.ProviderCurseForge && !curseforge {
			continue
		}
		providers = append(providers, provider)
	}
	return providers
}
