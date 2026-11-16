package routing

import "github.com/mclucy/lucy/catalog"

var searchCapabilityByProvider = map[catalog.Provider]map[catalog.Ecosystem]bool{
	catalog.ProviderModrinth: {
		catalog.EcosystemFabric:   true,
		catalog.EcosystemQuilt:    true,
		catalog.EcosystemForge:    true,
		catalog.EcosystemNeoForge: true,
		catalog.EcosystemBukkit:   true,
		catalog.EcosystemPaper:    true,
	},
	catalog.ProviderCurseForge: {
		catalog.EcosystemFabric:   true,
		catalog.EcosystemQuilt:    true,
		catalog.EcosystemForge:    true,
		catalog.EcosystemNeoForge: true,
	},
	catalog.ProviderHangar: {
		catalog.EcosystemBukkit: true,
		catalog.EcosystemPaper:  true,
	},
	catalog.ProviderSpiget: {
		catalog.EcosystemBukkit: true,
		catalog.EcosystemPaper:  true,
	},
	catalog.ProviderMCDR: {
		catalog.EcosystemMCDR: true,
	},
}

// EcosystemSupportedBy reports whether provider serves ecosystem.
func EcosystemSupportedBy(provider catalog.Provider, ecosystem catalog.Ecosystem) bool {
	return searchCapabilityByProvider[provider][ecosystem]
}
