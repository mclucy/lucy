package routing

import "github.com/mclucy/lucy/types"

// SearchPlatformSupport describes how a source can participate in search for a
// given platform.
type SearchPlatformSupport struct {
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
	Platforms map[types.PlatformId]SearchPlatformSupport
}

var unsupportedSearchPlatform = SearchPlatformSupport{}

var searchCapabilityBySource = map[types.SourceId]SourceSearchCapability{
	types.SourceModrinth: {
		Platforms: map[types.PlatformId]SearchPlatformSupport{
			types.PlatformFabric:   {Supported: true, UpstreamFilterable: true},
			types.PlatformForge:    {Supported: true, UpstreamFilterable: true},
			types.PlatformNeoforge: {Supported: true, UpstreamFilterable: true},
			types.PlatformBukkit:   {Supported: true, UpstreamFilterable: true},
		},
	},
	types.SourceCurseForge: {
		Platforms: map[types.PlatformId]SearchPlatformSupport{
			types.PlatformFabric:   {Supported: true, UpstreamFilterable: true},
			types.PlatformForge:    {Supported: true, UpstreamFilterable: true},
			types.PlatformNeoforge: {Supported: true, UpstreamFilterable: true},
			types.PlatformBukkit:   unsupportedSearchPlatform,
		},
	},
	types.SourceHangar: {
		Platforms: map[types.PlatformId]SearchPlatformSupport{
			types.PlatformFabric:   unsupportedSearchPlatform,
			types.PlatformForge:    unsupportedSearchPlatform,
			types.PlatformNeoforge: unsupportedSearchPlatform,
			types.PlatformBukkit:   {Supported: true, UpstreamFilterable: true},
		},
	},
	types.SourceSpiget: {
		Platforms: map[types.PlatformId]SearchPlatformSupport{
			types.PlatformFabric:   unsupportedSearchPlatform,
			types.PlatformForge:    unsupportedSearchPlatform,
			types.PlatformNeoforge: unsupportedSearchPlatform,
			types.PlatformBukkit: {
				Supported:          true,
				UpstreamFilterable: false,
			},
		},
	},
	types.SourceMCDR: {
		Platforms: map[types.PlatformId]SearchPlatformSupport{
			types.PlatformFabric:   unsupportedSearchPlatform,
			types.PlatformForge:    unsupportedSearchPlatform,
			types.PlatformNeoforge: unsupportedSearchPlatform,
			types.PlatformBukkit:   unsupportedSearchPlatform,
		},
	},
}

// SearchCapabilityFor returns static search capability metadata for a source.
func SearchCapabilityFor(src types.SourceId) (SourceSearchCapability, bool) {
	capability, ok := searchCapabilityBySource[src]
	return capability, ok
}

// PlatformSupportedBy returns the search support details for one source and
// platform combination.
func PlatformSupportedBy(
	src types.SourceId,
	platform types.PlatformId,
) (SearchPlatformSupport, bool) {
	capability, ok := SearchCapabilityFor(src)
	if !ok {
		return SearchPlatformSupport{}, false
	}

	support, ok := capability.Platforms[platform]
	return support, ok
}
