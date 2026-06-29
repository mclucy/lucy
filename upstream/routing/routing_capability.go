package routing

import "github.com/mclucy/lucy/types"

// SearchEcosystemSupport describes how a source can participate in search for a
// given ecosystem.
type SearchEcosystemSupport struct {
	// Supported reports whether the source can serve this platform at all.
	Supported bool
	// UpstreamFilterable reports whether the source can apply the platform filter
	// upstream instead of requiring post-filtering.
	UpstreamFilterable bool
}

// SourceSearchCapability describes static search capabilities for a source.
//
// This is a struct instead of an interface so additional capability dimensions
// can be added later without breaking callers.
type SourceSearchCapability struct {
	Ecosystems map[types.Ecosystem]SearchEcosystemSupport
}

var unsupportedSearchEcosystem = SearchEcosystemSupport{}

var searchCapabilityBySource = map[types.SourceId]SourceSearchCapability{
	types.SourceModrinth: {
		Ecosystems: map[types.Ecosystem]SearchEcosystemSupport{
			types.EcoFabric:   {Supported: true, UpstreamFilterable: true},
			types.EcoForge:    {Supported: true, UpstreamFilterable: true},
			types.EcoNeoforge: {Supported: true, UpstreamFilterable: true},
			types.EcoBukkit:   {Supported: true, UpstreamFilterable: true},
		},
	},
	types.SourceCurseForge: {
		Ecosystems: map[types.Ecosystem]SearchEcosystemSupport{
			types.EcoFabric:   {Supported: true, UpstreamFilterable: true},
			types.EcoForge:    {Supported: true, UpstreamFilterable: true},
			types.EcoNeoforge: {Supported: true, UpstreamFilterable: true},
			types.EcoBukkit:   unsupportedSearchEcosystem,
		},
	},
	types.SourceHangar: {
		Ecosystems: map[types.Ecosystem]SearchEcosystemSupport{
			types.EcoFabric:   unsupportedSearchEcosystem,
			types.EcoForge:    unsupportedSearchEcosystem,
			types.EcoNeoforge: unsupportedSearchEcosystem,
			types.EcoBukkit:   {Supported: true, UpstreamFilterable: true},
		},
	},
	types.SourceSpiget: {
		Ecosystems: map[types.Ecosystem]SearchEcosystemSupport{
			types.EcoFabric:   unsupportedSearchEcosystem,
			types.EcoForge:    unsupportedSearchEcosystem,
			types.EcoNeoforge: unsupportedSearchEcosystem,
			types.EcoBukkit: {
				Supported:          true,
				UpstreamFilterable: false,
			},
		},
	},
	types.SourceMCDR: {
		Ecosystems: map[types.Ecosystem]SearchEcosystemSupport{
			types.EcoFabric:   unsupportedSearchEcosystem,
			types.EcoForge:    unsupportedSearchEcosystem,
			types.EcoNeoforge: unsupportedSearchEcosystem,
			types.EcoBukkit:   unsupportedSearchEcosystem,
		},
	},
}

// SearchCapabilityFor returns static search capability metadata for a source.
func SearchCapabilityFor(src types.SourceId) (SourceSearchCapability, bool) {
	capability, ok := searchCapabilityBySource[src]
	return capability, ok
}

func EcosystemSupportedBy(
	src types.SourceId,
	ecosystem types.Ecosystem,
) (SearchEcosystemSupport, bool) {
	capability, ok := SearchCapabilityFor(src)
	if !ok {
		return SearchEcosystemSupport{}, false
	}

	support, ok := capability.Ecosystems[ecosystem]
	return support, ok
}
