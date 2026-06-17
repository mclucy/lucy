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
	"sync"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/providers/curseforge"
	"github.com/mclucy/lucy/upstream/providers/fabric"
	"github.com/mclucy/lucy/upstream/providers/forge"
	"github.com/mclucy/lucy/upstream/providers/githubsource"
	"github.com/mclucy/lucy/upstream/providers/hangar"
	"github.com/mclucy/lucy/upstream/providers/mcdr"
	"github.com/mclucy/lucy/upstream/providers/modrinth"
	"github.com/mclucy/lucy/upstream/providers/mojang"
	"github.com/mclucy/lucy/upstream/providers/neoforge"
	"github.com/mclucy/lucy/upstream/providers/spiget"
)

var (
	ErrUnknownSource     = errors.New("unknown source")
	ErrUnsupportedSource = errors.New("unsupported source")
	ErrInvalidPlatform   = errors.New("cannot find sources for platform")
)

type Registry struct {
	entries []entry
}

type entry struct {
	source types.SourceId
	impl   any
}

var defaultRegistry = sync.OnceValue(NewRegistry)

func DefaultRegistry() *Registry {
	return defaultRegistry()
}

func NewRegistry() *Registry {
	r := &Registry{}
	r.register(types.SourceModrinth, modrinth.Provider)
	r.register(types.SourceGitHub, githubsource.Provider)
	r.register(types.SourceMCDR, mcdr.Provider)
	r.register(types.SourceHangar, hangar.Provider)
	r.register(types.SourceSpiget, spiget.Provider)
	r.register(types.SourceMojang, mojang.Provider)
	r.register(types.SourceForge, forge.Provider)
	r.register(types.SourceNeoForge, neoforge.Provider)
	r.register(types.SourceFabric, fabric.Provider)
	if curseforge.Enabled() {
		r.register(types.SourceCurseForge, curseforge.Provider)
	}
	return r
}

func (r *Registry) register(source types.SourceId, impl any) {
	if impl == nil {
		return
	}
	r.entries = append(r.entries, entry{source: source, impl: impl})
}

func (r *Registry) has(source types.SourceId) bool {
	_, ok := r.entry(source)
	return ok
}

func (r *Registry) entry(source types.SourceId) (entry, bool) {
	for _, e := range r.entries {
		if e.source == source {
			return e, true
		}
	}
	return entry{}, false
}

func (r *Registry) PackageSource(
	source types.SourceId,
) (upstream.PackageSource, bool) {
	e, ok := r.entry(source)
	if !ok {
		return nil, false
	}
	provider, ok := e.impl.(upstream.PackageSource)
	return provider, ok
}

func (r *Registry) Searcher(
	source types.SourceId,
) (upstream.SearchSource, bool) {
	e, ok := r.entry(source)
	if !ok {
		return nil, false
	}
	provider, ok := e.impl.(upstream.SearchSource)
	return provider, ok
}

func (r *Registry) Informer(
	source types.SourceId,
) (upstream.InfoSource, bool) {
	e, ok := r.entry(source)
	if !ok {
		return nil, false
	}
	provider, ok := e.impl.(upstream.InfoSource)
	return provider, ok
}

func (r *Registry) ArtifactMapper(
	source types.SourceId,
) (upstream.ArtifactMapSource, bool) {
	e, ok := r.entry(source)
	if !ok {
		return nil, false
	}
	provider, ok := e.impl.(upstream.ArtifactMapSource)
	return provider, ok
}

func (r *Registry) PlatformInstaller(
	source types.SourceId,
) (upstream.PlatformInstaller, bool) {
	e, ok := r.entry(source)
	if !ok {
		return nil, false
	}
	installer, ok := e.impl.(upstream.PlatformInstaller)
	return installer, ok
}

func listModProviders() []upstream.PackageSource {
	providers, _ := providersFromSources(modProviderSources())
	return providers
}

// ListAutoProviders returns the default ordered provider list used when
// source=auto and platform=all.
func ListAutoProviders() []upstream.PackageSource {
	providers, _ := providersFromSources(autoProviderSources())
	return providers
}

func GetArtifactMapper(src types.SourceId) (upstream.ArtifactMapSource, bool, error) {
	mapper, ok := DefaultRegistry().ArtifactMapper(src)
	return mapper, ok, nil
}

func PlatformInstallerFor(
	platform types.PlatformId,
) (upstream.PlatformInstaller, bool) {
	source, ok := platformInstallerSource(platform)
	if !ok {
		return nil, false
	}
	return DefaultRegistry().PlatformInstaller(source)
}

func platformInstallerSource(platform types.PlatformId) (types.SourceId, bool) {
	switch platform {
	case types.PlatformMinecraft:
		return types.SourceMojang, true
	case types.PlatformForge:
		return types.SourceForge, true
	case types.PlatformNeoforge:
		return types.SourceNeoForge, true
	case types.PlatformFabric:
		return types.SourceFabric, true
	default:
		return types.SourceUnknown, false
	}
}

// ResolveProviders resolves ordered provider candidates for a given operation,
// platform, and user-specified source.
func ResolveProviders(
	platform types.PlatformId,
	src types.SourceId,
) ([]upstream.PackageSource, error) {
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
	platform types.PlatformId,
	src types.SourceId,
) ([]upstream.SearchSource, error) {
	if src == types.SourceUnknown {
		return nil, ErrUnknownSource
	}

	if src != types.SourceAuto {
		if err := validateSearchSourcePlatform(src, platform); err != nil {
			return nil, err
		}
		return resolveExplicitSearcher(src)
	}

	if !platform.IsSearchPlatform() {
		sources, err := providerSourcesForPlatform(platform)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", err, platform)
		}
		return searchersFromSources(sources)
	}

	sources := providerSourcesForSearchPlatform(platform)
	if len(sources) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrInvalidPlatform, platform)
	}
	return searchersFromSources(sources)
}

func ResolveInfoProviders(
	platform types.PlatformId,
	src types.SourceId,
) ([]upstream.InfoSource, error) {
	if src == types.SourceUnknown {
		return nil, ErrUnknownSource
	}

	if src != types.SourceAuto {
		return resolveExplicitInformer(src)
	}

	sources, err := providerSourcesForPlatform(platform)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", err, platform)
	}
	return informersFromSources(sources)
}

func resolveExplicitInformer(src types.SourceId) ([]upstream.InfoSource, error) {
	provider, ok := DefaultRegistry().Informer(src)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedSource, src)
	}
	return []upstream.InfoSource{provider}, nil
}

func resolveExplicitSearcher(src types.SourceId) ([]upstream.SearchSource, error) {
	provider, ok := DefaultRegistry().Searcher(src)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedSource, src)
	}
	return []upstream.SearchSource{provider}, nil
}

func ResolveProvidersFromTopology(
	topology *types.RuntimeTopology,
	src types.SourceId,
) ([]upstream.PackageSource, error) {
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
	return []upstream.PackageSource{}, nil
}

func resolveExplicitSource(src types.SourceId) ([]upstream.PackageSource, error) {
	provider, ok := DefaultRegistry().PackageSource(src)
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedSource, src)
	}
	return []upstream.PackageSource{provider}, nil
}

func validateSearchSourcePlatform(
	src types.SourceId,
	platform types.PlatformId,
) error {
	if !platform.IsSearchPlatform() {
		return nil
	}

	support, ok := PlatformSupportedBy(src, platform)
	if ok && support.Supported {
		return nil
	}

	return fmt.Errorf("source %s does not support platform %s", src, platform)
}

func providersFromSources(sources []types.SourceId) (
	[]upstream.PackageSource,
	error,
) {
	providers := make([]upstream.PackageSource, 0, len(sources))
	for _, source := range sources {
		provider, ok := DefaultRegistry().PackageSource(source)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedSource, source)
		}
		providers = append(providers, provider)
	}
	return providers, nil
}

func searchersFromSources(sources []types.SourceId) (
	[]upstream.SearchSource,
	error,
) {
	providers := make([]upstream.SearchSource, 0, len(sources))
	for _, source := range sources {
		provider, ok := DefaultRegistry().Searcher(source)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedSource, source)
		}
		providers = append(providers, provider)
	}
	return providers, nil
}

func informersFromSources(sources []types.SourceId) (
	[]upstream.InfoSource,
	error,
) {
	providers := make([]upstream.InfoSource, 0, len(sources))
	for _, source := range sources {
		provider, ok := DefaultRegistry().Informer(source)
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedSource, source)
		}
		providers = append(providers, provider)
	}
	return providers, nil
}

func curseforgeAvailable() bool {
	return DefaultRegistry().has(types.SourceCurseForge)
}
