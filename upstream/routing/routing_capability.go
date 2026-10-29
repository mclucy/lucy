package routing

import "github.com/mclucy/lucy/catalog"

// SearchEcosystemSupport describes how a provider can participate in search for a
// given ecosystem.
type SearchEcosystemSupport struct {
	// Supported reports whether the provider can serve this platform at all.
	Supported bool
	// UpstreamFilterable reports whether the provider can apply the platform filter
	// upstream instead of requiring post-filtering.
	UpstreamFilterable bool
}

// SearchCapability describes static search capabilities for a provider.
//
// This is a struct instead of an interface so additional capability dimensions
// can be added later without breaking callers.
type SearchCapability struct {
	Ecosystems map[catalog.Ecosystem]SearchEcosystemSupport
}

var unsupportedSearchEcosystem = SearchEcosystemSupport{}

var searchCapabilityByProvider = map[catalog.Provider]SearchCapability{
	catalog.ProviderModrinth: {
		Ecosystems: map[catalog.Ecosystem]SearchEcosystemSupport{
			catalog.EcosystemFabric:   {Supported: true, UpstreamFilterable: true},
			catalog.EcosystemForge:    {Supported: true, UpstreamFilterable: true},
			catalog.EcosystemNeoForge: {Supported: true, UpstreamFilterable: true},
			catalog.EcosystemBukkit:   {Supported: true, UpstreamFilterable: true},
			catalog.EcosystemPaper:    {Supported: true, UpstreamFilterable: true},
		},
	},
	catalog.ProviderCurseForge: {
		Ecosystems: map[catalog.Ecosystem]SearchEcosystemSupport{
			catalog.EcosystemFabric:   {Supported: true, UpstreamFilterable: true},
			catalog.EcosystemForge:    {Supported: true, UpstreamFilterable: true},
			catalog.EcosystemNeoForge: {Supported: true, UpstreamFilterable: true},
			catalog.EcosystemBukkit:   unsupportedSearchEcosystem,
		},
	},
	catalog.ProviderHangar: {
		Ecosystems: map[catalog.Ecosystem]SearchEcosystemSupport{
			catalog.EcosystemFabric:   unsupportedSearchEcosystem,
			catalog.EcosystemForge:    unsupportedSearchEcosystem,
			catalog.EcosystemNeoForge: unsupportedSearchEcosystem,
			catalog.EcosystemBukkit:   {Supported: true, UpstreamFilterable: true},
			catalog.EcosystemPaper:    {Supported: true, UpstreamFilterable: true},
		},
	},
	catalog.ProviderSpiget: {
		Ecosystems: map[catalog.Ecosystem]SearchEcosystemSupport{
			catalog.EcosystemFabric:   unsupportedSearchEcosystem,
			catalog.EcosystemForge:    unsupportedSearchEcosystem,
			catalog.EcosystemNeoForge: unsupportedSearchEcosystem,
			catalog.EcosystemBukkit: {
				Supported:          true,
				UpstreamFilterable: false,
			},
			catalog.EcosystemPaper: {
				Supported:          true,
				UpstreamFilterable: false,
			},
		},
	},
	catalog.ProviderMCDR: {
		Ecosystems: map[catalog.Ecosystem]SearchEcosystemSupport{
			catalog.EcosystemFabric:   unsupportedSearchEcosystem,
			catalog.EcosystemForge:    unsupportedSearchEcosystem,
			catalog.EcosystemNeoForge: unsupportedSearchEcosystem,
			catalog.EcosystemBukkit:   unsupportedSearchEcosystem,
		},
	},
}

// SearchCapabilityFor returns static search capability metadata for a provider.
func SearchCapabilityFor(provider catalog.Provider) (SearchCapability, bool) {
	capability, ok := searchCapabilityByProvider[provider]
	return capability, ok
}

func EcosystemSupportedBy(
	provider catalog.Provider,
	ecosystem catalog.Ecosystem,
) (SearchEcosystemSupport, bool) {
	capability, ok := SearchCapabilityFor(provider)
	if !ok {
		return SearchEcosystemSupport{}, false
	}

	support, ok := capability.Ecosystems[ecosystem]
	return support, ok
}
