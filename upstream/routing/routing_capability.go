package routing

import "github.com/mclucy/lucy/types"

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
	Ecosystems map[types.Ecosystem]SearchEcosystemSupport
}

var unsupportedSearchEcosystem = SearchEcosystemSupport{}

var searchCapabilityByProvider = map[types.Provider]SearchCapability{
	types.ProviderModrinth: {
		Ecosystems: map[types.Ecosystem]SearchEcosystemSupport{
			types.EcosystemFabric:   {Supported: true, UpstreamFilterable: true},
			types.EcosystemForge:    {Supported: true, UpstreamFilterable: true},
			types.EcosystemNeoForge: {Supported: true, UpstreamFilterable: true},
			types.EcosystemBukkit:   {Supported: true, UpstreamFilterable: true},
			types.EcosystemPaper:    {Supported: true, UpstreamFilterable: true},
		},
	},
	types.ProviderCurseForge: {
		Ecosystems: map[types.Ecosystem]SearchEcosystemSupport{
			types.EcosystemFabric:   {Supported: true, UpstreamFilterable: true},
			types.EcosystemForge:    {Supported: true, UpstreamFilterable: true},
			types.EcosystemNeoForge: {Supported: true, UpstreamFilterable: true},
			types.EcosystemBukkit:   unsupportedSearchEcosystem,
		},
	},
	types.ProviderHangar: {
		Ecosystems: map[types.Ecosystem]SearchEcosystemSupport{
			types.EcosystemFabric:   unsupportedSearchEcosystem,
			types.EcosystemForge:    unsupportedSearchEcosystem,
			types.EcosystemNeoForge: unsupportedSearchEcosystem,
			types.EcosystemBukkit:   {Supported: true, UpstreamFilterable: true},
			types.EcosystemPaper:    {Supported: true, UpstreamFilterable: true},
		},
	},
	types.ProviderSpiget: {
		Ecosystems: map[types.Ecosystem]SearchEcosystemSupport{
			types.EcosystemFabric:   unsupportedSearchEcosystem,
			types.EcosystemForge:    unsupportedSearchEcosystem,
			types.EcosystemNeoForge: unsupportedSearchEcosystem,
			types.EcosystemBukkit: {
				Supported:          true,
				UpstreamFilterable: false,
			},
			types.EcosystemPaper: {
				Supported:          true,
				UpstreamFilterable: false,
			},
		},
	},
	types.ProviderMCDR: {
		Ecosystems: map[types.Ecosystem]SearchEcosystemSupport{
			types.EcosystemFabric:   unsupportedSearchEcosystem,
			types.EcosystemForge:    unsupportedSearchEcosystem,
			types.EcosystemNeoForge: unsupportedSearchEcosystem,
			types.EcosystemBukkit:   unsupportedSearchEcosystem,
		},
	},
}

// SearchCapabilityFor returns static search capability metadata for a provider.
func SearchCapabilityFor(provider types.Provider) (SearchCapability, bool) {
	capability, ok := searchCapabilityByProvider[provider]
	return capability, ok
}

func EcosystemSupportedBy(
	provider types.Provider,
	ecosystem types.Ecosystem,
) (SearchEcosystemSupport, bool) {
	capability, ok := SearchCapabilityFor(provider)
	if !ok {
		return SearchEcosystemSupport{}, false
	}

	support, ok := capability.Ecosystems[ecosystem]
	return support, ok
}
