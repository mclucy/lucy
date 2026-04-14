package routing

import (
	"fmt"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
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

	sources := providerSourcesByCapability(topology)
	if len(sources) == 0 {
		return nil, fmt.Errorf("%w: no providers resolved from topology", ErrInvalidPlatform)
	}

	return providersFromSources(sources)
}
