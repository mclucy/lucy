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

func providerSourcesForEcosystem(ecosystem types.Ecosystem) (
	[]types.SourceId,
	error,
) {
	switch ecosystem {
	case types.EcoUnspecified:
		return autoProviderSources(), nil
	case types.EcoMcdr:
		return []types.SourceId{types.SourceMCDR}, nil
	case types.EcoForge, types.EcoFabric, types.EcoNeoforge, types.EcoBukkit:
		return providerSourcesForSearchEcosystem(ecosystem), nil
	default:
		return nil, ErrInvalidEcosystem
	}
}

func providerSourcesForSearchEcosystem(ecosystem types.Ecosystem) []types.SourceId {
	sources := make(
		[]types.SourceId,
		0,
		len(searchProviderSourcesInPriorityOrder),
	)
	for _, source := range searchProviderSourcesInPriorityOrder {
		if source == types.SourceCurseForge && !curseforgeAvailable() {
			continue
		}

		support, ok := EcosystemSupportedBy(source, ecosystem)
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
	case types.CapabilityBukkitAPI,
		types.CapabilitySpigotAPI,
		types.CapabilityPaperAPI,
		types.CapabilityPurpurAPI,
		types.CapabilityFoliaAPI:
		return true
	default:
		return false
	}
}

func providerSourcesFromTopology(topology *types.ServerTopology) topologyResolution {
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
			case types.CapabilityFabricLoader,
				types.CapabilityForge,
				types.CapabilityNeoforge:
				sawKnownCapability = true
				appendSource(types.SourceModrinth)
				if curseforgeAvailable() {
					appendSource(types.SourceCurseForge)
				}
			case types.CapabilityBukkitAPI,
				types.CapabilitySpigotAPI,
				types.CapabilityPaperAPI,
				types.CapabilityPurpurAPI,
				types.CapabilityFoliaAPI:
				sawKnownCapability = true
				appendSource(types.SourceModrinth)
				if isBukkitFamilyCapability(capability) {
					appendSource(types.SourceHangar)
					appendSource(types.SourceSpiget)
				}
			case types.CapabilityMcdr:
				sawKnownCapability = true
				appendSource(types.SourceMCDR)
			case types.CapabilityReversedProxy:
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
