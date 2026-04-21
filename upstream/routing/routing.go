// Package routing contains source-to-provider bindings and source resolution
// policies.
//
// Responsibilities:
//   - Resolve SourceAuto against Platform into ordered provider candidates.
//   - Map explicit Source to exactly one provider when supported.
//   - Apply operation-aware routing policy (search/info/fetch/dependencies).
//   - Return typed selection errors for invalid/unsupported inputs.
//
// Non-responsibilities:
//   - Do not call provider APIs.
//   - Do not aggregate or merge upstream result payloads.
package routing

import (
	"errors"
	"fmt"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/curseforge"
	"github.com/mclucy/lucy/upstream/githubsource"
	"github.com/mclucy/lucy/upstream/hangar"
	"github.com/mclucy/lucy/upstream/mcdr"
	"github.com/mclucy/lucy/upstream/modrinth"
	"github.com/mclucy/lucy/upstream/spiget"
)

var (
	ErrUnknownSource     = errors.New("unknown source")
	ErrUnsupportedSource = errors.New("unsupported source")
	ErrInvalidPlatform   = errors.New("cannot find sources for platform")
)

// providerBySource binds semantic Source values to executable Provider
// implementations.
//
// Source and Provider are intentionally not synonyms:
//   - Some Source values are policy/sentinel markers (SourceAuto/SourceUnknown).
//   - A Source can resolve to one provider, many providers, or none.
var providerBySource = map[types.Source]upstream.Provider{
	types.SourceModrinth: modrinth.Provider,
	types.SourceGitHub:   githubsource.Provider,
	types.SourceMCDR:     mcdr.Provider,
	types.SourceHangar:   hangar.Provider,
	types.SourceSpiget:   spiget.Provider,
}

func listModProviders() []upstream.Provider {
	providers, _ := providersFromSources(modProviderSources())
	return providers
}

// ListAutoProviders returns the default ordered provider list used when
// source=auto and platform=all.
func ListAutoProviders() []upstream.Provider {
	providers := listModProviders()
	providers, _ = providersFromSources(append(modProviderSources(), types.SourceMCDR))
	return providers
}

func GetProvider(src types.Source) (upstream.Provider, bool, error) {
	if src == types.SourceCurseForge {
		if err := curseforge.AvailabilityError(); err != nil {
			return nil, false, err
		}
		return curseforge.Provider, true, nil
	}

	p, ok := providerBySource[src]
	return p, ok, nil
}

// ResolveProviders resolves ordered provider candidates for a given operation,
// platform, and user-specified source.
func ResolveProviders(
	platform types.Platform,
	src types.Source,
) ([]upstream.Provider, error) {
	if src == types.SourceUnknown {
		return nil, ErrUnknownSource
	}

	if src != types.SourceAuto {
		return resolveExplicitSource(src)
	}

	sources, err := providerSourcesForPlatform(platform)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, platform)
	}
	return providersFromSources(sources)
}

// ResolveSearchProviders resolves providers for search operations. When a
// specific platform filter is active, routing validates explicit source
// selection and uses source capability data as the authority for automatic
// selection.
func ResolveSearchProviders(
	platform types.Platform,
	src types.Source,
) ([]upstream.Provider, error) {
	if src == types.SourceUnknown {
		return nil, ErrUnknownSource
	}

	if src != types.SourceAuto {
		if err := validateSearchSourcePlatform(src, platform); err != nil {
			return nil, err
		}
		return resolveExplicitSource(src)
	}

	if !platform.IsSearchPlatform() {
		return ResolveProviders(platform, src)
	}

	sources := providerSourcesForSearchPlatform(platform)
	if len(sources) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidPlatform, platform)
	}
	return providersFromSources(sources)
}

func ResolveProvidersFromTopology(
	topology *types.RuntimeTopology,
	src types.Source,
) ([]upstream.Provider, error) {
	if src == types.SourceUnknown {
		return nil, ErrUnknownSource
	}

	if src != types.SourceAuto {
		return resolveExplicitSource(src)
	}

	if topology == nil || !topology.Resolved() {
		return nil, fmt.Errorf("routing: topology unresolved, cannot resolve providers")
	}

	selection := providerSourcesFromTopology(topology)
	if len(selection.sources) > 0 {
		return providersFromSources(selection.sources)
	}
	if selection.fallback {
		return ListAutoProviders(), nil
	}
	return []upstream.Provider{}, nil
}

func resolveExplicitSource(src types.Source) ([]upstream.Provider, error) {
	provider, ok, err := GetProvider(src)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedSource, src)
	}

	return []upstream.Provider{provider}, nil
}

func validateSearchSourcePlatform(src types.Source, platform types.Platform) error {
	if !platform.IsSearchPlatform() {
		return nil
	}

	support, ok := PlatformSupportedBy(src, platform)
	if ok && support.Supported {
		return nil
	}

	return fmt.Errorf("source %s does not support platform %s", src, platform)
}

func providersFromSources(sources []types.Source) ([]upstream.Provider, error) {
	providers := make([]upstream.Provider, 0, len(sources))
	for _, source := range sources {
		provider, ok, err := GetProvider(source)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedSource, source)
		}
		providers = append(providers, provider)
	}
	return providers, nil
}

func curseforgeAvailable() bool {
	return curseforge.Enabled()
}
