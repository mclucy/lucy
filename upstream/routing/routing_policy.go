package routing

import "github.com/mclucy/lucy/types"

func autoProviderSources() []types.Source {
	return append(modProviderSources(), types.SourceMCDR)
}

func modProviderSources() []types.Source {
	sources := []types.Source{types.SourceModrinth}
	if curseforgeAvailable() {
		sources = append(sources, types.SourceCurseForge)
	}
	return sources
}

func providerSourcesForPlatform(platform types.Platform) ([]types.Source, error) {
	switch platform {
	case types.PlatformAny:
		return autoProviderSources(), nil
	case types.PlatformForge, types.PlatformFabric, types.PlatformNeoforge:
		return modProviderSources(), nil
	case types.PlatformBukkit:
		return []types.Source{types.SourceModrinth, types.SourceHangar, types.SourceSpiget}, nil
	case types.PlatformMCDR:
		return []types.Source{types.SourceMCDR}, nil
	default:
		return nil, ErrInvalidPlatform
	}
}

type topologyResolution struct {
	sources  []types.Source
	fallback bool
	empty    bool
}

func providerSourcesFromTopology(topology *types.RuntimeTopology) topologyResolution {
	selection := topologyResolution{}
	seen := map[types.Source]struct{}{}
	sawKnownCapability := false
	sawProxyCapability := false

	appendSource := func(source types.Source) {
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
				types.CapabilityNeoforgeMods,
				types.CapabilityBukkitPlugins:
				sawKnownCapability = true
				appendSource(types.SourceModrinth)
				if capability != types.CapabilityBukkitPlugins && curseforgeAvailable() {
					appendSource(types.SourceCurseForge)
				}
				if capability == types.CapabilityBukkitPlugins {
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

func providerSourcesByCapability(topology *types.RuntimeTopology) []types.Source {
	sources := make([]types.Source, 0, 2)
	seen := map[types.Source]struct{}{}

	appendSource := func(source types.Source) {
		if _, exists := seen[source]; exists {
			return
		}
		seen[source] = struct{}{}
		sources = append(sources, source)
	}

	if topology.HasCapability(types.CapabilityFabricMods) ||
		topology.HasCapability(types.CapabilityForgeMods) ||
		topology.HasCapability(types.CapabilityNeoforgeMods) {
		appendSource(types.SourceModrinth)
		if curseforgeAvailable() {
			appendSource(types.SourceCurseForge)
		}
	}

	if topology.HasCapability(types.CapabilityBukkitPlugins) {
		appendSource(types.SourceModrinth)
		appendSource(types.SourceHangar)
		appendSource(types.SourceSpiget)
	}

	if topology.HasCapability(types.CapabilityMCDRPlugins) {
		appendSource(types.SourceMCDR)
	}

	if topology.HasCapability(types.CapabilityProxying) {
		appendSource(types.SourceModrinth)
	}

	return sources
}
