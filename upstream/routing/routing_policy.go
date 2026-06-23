package routing

import "github.com/mclucy/lucy/types"

var searchProviderSourcesInPriorityOrder = []types.SourceId{
	types.SourceModrinth,
	types.SourceCurseForge,
	types.SourceHangar,
	types.SourceSpiget,
}

func autoProviderSources() []types.SourceId {
	return append(modProviderSources(), types.SourceMCDR)
}

func modProviderSources() []types.SourceId {
	sources := []types.SourceId{types.SourceModrinth}
	if curseforgeAvailable() {
		sources = append(sources, types.SourceCurseForge)
	}
	return sources
}

func providerSourcesForPlatform(platform types.PlatformId) (
	[]types.SourceId,
	error,
) {
	switch platform {
	case types.PlatformAny:
		return autoProviderSources(), nil
	case types.PlatformMCDR:
		return []types.SourceId{types.SourceMCDR}, nil
	case types.PlatformForge, types.PlatformFabric, types.PlatformNeoforge, types.PlatformBukkit:
		return providerSourcesForSearchPlatform(platform), nil
	default:
		return nil, ErrInvalidPlatform
	}
}

func providerSourcesForSearchPlatform(platform types.PlatformId) []types.SourceId {
	sources := make(
		[]types.SourceId,
		0,
		len(searchProviderSourcesInPriorityOrder),
	)
	for _, source := range searchProviderSourcesInPriorityOrder {
		if source == types.SourceCurseForge && !curseforgeAvailable() {
			continue
		}

		support, ok := PlatformSupportedBy(source, platform)
		if !ok || !support.Supported {
			continue
		}

		sources = append(sources, source)
	}
	return sources
}

type topologyResolution struct {
	sources  []types.SourceId
	fallback bool
	empty    bool
}

// isBukkitFamilyCapability reports whether capability is a Bukkit-family
// plugin capability. Routing policy, not a type-system property.
func isBukkitFamilyCapability(capability types.RuntimeCapability) bool {
	switch capability {
	case types.CapabilityBukkitPlugins,
		types.CapabilitySpigotPlugins,
		types.CapabilityPaperPlugins,
		types.CapabilityPurpurPlugins,
		types.CapabilityFoliaPlugins:
		return true
	default:
		return false
	}
}

func providerSourcesFromTopology(topology *types.RuntimeTopology) topologyResolution {
	selection := topologyResolution{}
	seen := map[types.SourceId]struct{}{}
	sawKnownCapability := false
	sawProxyCapability := false

	appendSource := func(source types.SourceId) {
		if _, ok := seen[source]; ok {
			return
		}
		seen[source] = struct{}{}
		selection.sources = append(selection.sources, source)
	}

	for _, node := range topology.Nodes {
		for _, capability := range node.Capabilities {
			switch capability {
			case types.CapabilityFabricMods,
				types.CapabilityForgeMods,
				types.CapabilityNeoforgeMods:
				sawKnownCapability = true
				appendSource(types.SourceModrinth)
				if curseforgeAvailable() {
					appendSource(types.SourceCurseForge)
				}
			case types.CapabilityBukkitPlugins,
				types.CapabilitySpigotPlugins,
				types.CapabilityPaperPlugins,
				types.CapabilityPurpurPlugins,
				types.CapabilityFoliaPlugins:
				sawKnownCapability = true
				appendSource(types.SourceModrinth)
				if isBukkitFamilyCapability(capability) {
					appendSource(types.SourceHangar)
					appendSource(types.SourceSpiget)
				}
			case types.CapabilityMCDRPlugins:
				sawKnownCapability = true
				appendSource(types.SourceMCDR)
			case types.CapabilityProxying:
				sawKnownCapability = true
				sawProxyCapability = true
			}
		}
	}

	if len(selection.sources) > 0 {
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
