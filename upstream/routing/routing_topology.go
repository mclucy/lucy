package routing

import (
	"fmt"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/curseforge"
	"github.com/mclucy/lucy/upstream/mcdr"
	"github.com/mclucy/lucy/upstream/modrinth"
)

// ResolveProvidersByTopology resolves providers using runtime topology
// capabilities. Returns an error when topology is nil/unresolved.
// Explicit source selection always delegates to ResolveProviders.
func ResolveProvidersByTopology(
	topology *types.RuntimeTopology,
	platform types.Platform,
	src types.Source,
) ([]upstream.Provider, error) {
	if topology == nil || !topology.Resolved() {
		return nil, fmt.Errorf("routing: topology unresolved, cannot resolve providers")
	}

	if src != types.SourceAuto {
		return ResolveProviders(platform, src)
	}

	if platform == types.PlatformAny {
		return ListAutoProviders(), nil
	}

	providers := resolveProvidersByCapability(topology)
	if len(providers) == 0 {
		return nil, fmt.Errorf("%w: no providers resolved from topology", ErrInvalidPlatform)
	}

	return providers, nil
}

func resolveProvidersByCapability(topology *types.RuntimeTopology) []upstream.Provider {
	providers := make([]upstream.Provider, 0, 2)
	seen := map[types.Source]struct{}{}

	addProvider := func(provider upstream.Provider) {
		source := provider.Source()
		if _, exists := seen[source]; exists {
			return
		}
		seen[source] = struct{}{}
		providers = append(providers, provider)
	}

	if topology.HasCapability(types.CapabilityFabricMods) ||
		topology.HasCapability(types.CapabilityForgeMods) ||
		topology.HasCapability(types.CapabilityNeoforgeMods) {
		addProvider(modrinth.Provider)
		if curseforge.Enabled() {
			addProvider(curseforge.Provider)
		}
	}

	if topology.HasCapability(types.CapabilityBukkitPlugins) {
		addProvider(modrinth.Provider)
	}

	if topology.HasCapability(types.CapabilityMCDRPlugins) {
		addProvider(mcdr.Provider)
	}

	if topology.HasCapability(types.CapabilityProxying) {
		addProvider(modrinth.Provider)
	}

	return providers
}
