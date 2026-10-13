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

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/providers/bmclapi"
	"github.com/mclucy/lucy/upstream/providers/curseforge"
	"github.com/mclucy/lucy/upstream/providers/fabric"
	"github.com/mclucy/lucy/upstream/providers/forge"
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
	ErrInvalidEcosystem  = errors.New("cannot find sources for ecosystem")
	ErrNoCatalogue       = errors.New("source has no package catalogue")
)

type providerCatalog struct {
	packageSources     map[types.SourceId]upstream.PackageSource
	searchSources      map[types.SourceId]upstream.SearchSource
	infoSources        map[types.SourceId]upstream.InfoSource
	artifactMappers    map[types.SourceId]upstream.ArtifactMapSource
	ecosystemProviders map[types.SourceId]upstream.EcosystemProvider
}

var providers = sync.OnceValue(newProviderCatalog)

func newProviderCatalog() providerCatalog {
	catalog := providerCatalog{
		packageSources: map[types.SourceId]upstream.PackageSource{
			types.SourceModrinth: modrinth.Provider,
			types.SourceMCDR:     mcdr.Provider,
			types.SourceHangar:   hangar.Provider,
			types.SourceSpiget:   spiget.Provider,
		},
		searchSources: map[types.SourceId]upstream.SearchSource{
			types.SourceModrinth: modrinth.Provider,
			types.SourceMCDR:     mcdr.Provider,
			types.SourceHangar:   hangar.Provider,
			types.SourceSpiget:   spiget.Provider,
		},
		infoSources: map[types.SourceId]upstream.InfoSource{
			types.SourceModrinth: modrinth.Provider,
			types.SourceMCDR:     mcdr.Provider,
			types.SourceHangar:   hangar.Provider,
			types.SourceSpiget:   spiget.Provider,
		},
		artifactMappers: map[types.SourceId]upstream.ArtifactMapSource{
			types.SourceModrinth: modrinth.Provider,
		},
		ecosystemProviders: map[types.SourceId]upstream.EcosystemProvider{
			types.SourceBMCLAPI:  bmclapi.Provider,
			types.SourceFabric:   fabric.Provider,
			types.SourceForge:    forge.Provider,
			types.SourceMojang:   mojang.Provider,
			types.SourceNeoForge: neoforge.Provider,
		},
	}
	if !curseforge.Enabled() {
		return catalog
	}

	catalog.packageSources[types.SourceCurseForge] = curseforge.Provider
	catalog.searchSources[types.SourceCurseForge] = curseforge.Provider
	catalog.infoSources[types.SourceCurseForge] = curseforge.Provider
	catalog.artifactMappers[types.SourceCurseForge] = curseforge.Provider
	return catalog
}

func providerCatalogInstance() providerCatalog {
	return providers()
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

func GetArtifactMapper(
	src types.SourceId,
) (upstream.ArtifactMapSource, bool, error) {
	mapper, ok := providerCatalogInstance().artifactMappers[src]
	if !ok {
		log.Debug(
			"routing: no artifact mapper for source",
			"source", src,
			"reason", "source has no artifact mapper",
		)
	}
	return mapper, ok, nil
}

func artifactMappers() []upstream.ArtifactMapSource {
	return artifactMappersFromSources(modProviderSources())
}

func artifactMappersFromSources(sources []types.SourceId) []upstream.ArtifactMapSource {
	catalog := providerCatalogInstance()
	mappers := make([]upstream.ArtifactMapSource, 0, len(sources))
	for _, source := range sources {
		mapper, ok := catalog.artifactMappers[source]
		if ok {
			mappers = append(mappers, mapper)
		}
	}
	return mappers
}

func EcosystemInstallerFor(
	ecosystem types.Ecosystem,
) (upstream.EcosystemProvider, bool) {
	source, ok := ecosystemInstallerSource(ecosystem)
	if !ok {
		return nil, false
	}
	return EcosystemInstallerForSource(source)
}

func EcosystemInstallerForSource(
	source types.SourceId,
) (upstream.EcosystemProvider, bool) {
	provider, ok := providerCatalogInstance().ecosystemProviders[source]
	if !ok {
		log.Debug(
			"routing: no ecosystem installer",
			"source", source,
			"reason", "source has no ecosystem provider",
		)
	}
	return provider, ok
}

func ecosystemInstallerSource(ecosystem types.Ecosystem) (
	types.SourceId,
	bool,
) {
	switch ecosystem {
	case types.EcoMinecraft:
		return types.SourceMojang, true
	case types.EcoForge:
		return types.SourceForge, true
	case types.EcoNeoforge:
		return types.SourceNeoForge, true
	case types.EcoFabric:
		return types.SourceFabric, true
	default:
		log.Debug(
			"routing: no ecosystem installer",
			"ecosystem", ecosystem,
			"reason", "unsupported ecosystem",
		)
		return types.SourceUnknown, false
	}
}

// ResolveProviders resolves ordered provider candidates for the platform and
// user-specified source.
func ResolveProviders(
	platform types.Ecosystem,
	src types.SourceId,
) ([]upstream.PackageSource, error) {
	if src == types.SourceUnknown {
		return nil, ErrUnknownSource
	}

	if src != types.SourceAuto {
		return resolveExplicitSource(src)
	}

	sources, err := providerSourcesForEcosystem(platform)
	if err != nil {
		log.Debug(
			"routing: package providers unavailable for ecosystem",
			"platform", platform,
			"reason", "unsupported ecosystem",
		)
		return nil, fmt.Errorf("%w: %s", err, platform)
	}
	providers, err := providersFromSources(sources)
	if err != nil {
		return nil, err
	}
	log.Debug(
		"routing: package providers selected from ecosystem candidates",
		"platform", platform,
		"sources", sources,
	)
	return providers, nil
}

// ResolveSearchProviders resolves search providers for the platform. With a
// platform filter active, explicit source selection is validated against
// source capability data.
func ResolveSearchProviders(
	platform types.Ecosystem,
	src types.SourceId,
) ([]upstream.SearchSource, error) {
	if src == types.SourceUnknown {
		return nil, ErrUnknownSource
	}

	if src != types.SourceAuto {
		if err := validateSearchSourceEcosystem(src, platform); err != nil {
			log.Debug(
				"routing: search source rejected for platform",
				"source", src,
				"platform", platform,
				"reason", "source does not support platform",
			)
			return nil, err
		}
		return resolveExplicitSearcher(src)
	}

	if !platform.IsSearchEcosystem() {
		sources, err := providerSourcesForEcosystem(platform)
		if err != nil {
			log.Debug(
				"routing: search providers unavailable for ecosystem",
				"platform", platform,
				"reason", "unsupported ecosystem",
			)
			return nil, fmt.Errorf("%w: %s", err, platform)
		}
		providers, err := searchersFromSources(sources)
		if err != nil {
			return nil, err
		}
		log.Debug(
			"routing: search providers selected from ecosystem candidates",
			"platform", platform,
			"sources", sources,
		)
		return providers, nil
	}

	sources := providerSourcesForSearchEcosystem(platform)
	if len(sources) == 0 {
		log.Debug(
			"routing: search providers unavailable for platform",
			"platform", platform,
			"reason", "no source supports platform search",
		)
		return nil, fmt.Errorf("%w: %s", ErrInvalidEcosystem, platform)
	}
	providers, err := searchersFromSources(sources)
	if err != nil {
		return nil, err
	}
	log.Debug(
		"routing: search providers selected for platform",
		"platform", platform,
		"sources", sources,
	)
	return providers, nil
}

func ResolveInfoProviders(
	platform types.Ecosystem,
	src types.SourceId,
) ([]upstream.InfoSource, error) {
	if src == types.SourceUnknown {
		return nil, ErrUnknownSource
	}

	if src != types.SourceAuto {
		return resolveExplicitInformer(src)
	}

	sources, err := providerSourcesForEcosystem(platform)
	if err != nil {
		log.Debug(
			"routing: info providers unavailable for ecosystem",
			"platform", platform,
			"reason", "unsupported ecosystem",
		)
		return nil, fmt.Errorf("%w: %s", err, platform)
	}
	providers, err := informersFromSources(sources)
	if err != nil {
		return nil, err
	}
	log.Debug(
		"routing: info providers selected from ecosystem candidates",
		"platform", platform,
		"sources", sources,
	)
	return providers, nil
}

func resolveExplicitInformer(src types.SourceId) (
	[]upstream.InfoSource,
	error,
) {
	provider, ok := providerCatalogInstance().infoSources[src]
	if !ok {
		log.Debug(
			"routing: explicit info source rejected",
			"source", src,
			"reason", "source has no info provider",
		)
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedSource, src)
	}
	log.Debug(
		"routing: explicit info source selected",
		"source", src,
		"provider", provider.Id(),
	)
	return []upstream.InfoSource{provider}, nil
}

func resolveExplicitSearcher(src types.SourceId) (
	[]upstream.SearchSource,
	error,
) {
	provider, ok := providerCatalogInstance().searchSources[src]
	if !ok {
		log.Debug(
			"routing: explicit search source rejected",
			"source", src,
			"reason", "source has no search provider",
		)
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedSource, src)
	}
	log.Debug(
		"routing: explicit search source selected",
		"source", src,
		"provider", provider.Id(),
	)
	return []upstream.SearchSource{provider}, nil
}

func ResolveProvidersForRuntime(
	ecosystems []types.Ecosystem,
	src types.SourceId,
) ([]upstream.PackageSource, error) {
	if src == types.SourceUnknown {
		return nil, ErrUnknownSource
	}

	if src != types.SourceAuto {
		return resolveExplicitSource(src)
	}

	if len(ecosystems) == 0 {
		log.Debug(
			"routing: package providers unavailable for runtime",
			"reason", "no runtime ecosystems",
		)
		return nil, fmt.Errorf("routing: runtime ecosystems unavailable, cannot resolve providers")
	}

	selection := providerSourcesFromEcosystems(ecosystems)
	switch {
	case len(selection.sources) > 0:
		log.Debug(
			"routing: package providers selected from runtime ecosystems",
			"ecosystems", ecosystems,
			"sources", selection.sources,
		)
		return providersFromSources(selection.sources)
	case selection.fallback:
		log.Debug(
			"routing: package providers fell back to auto list",
			"ecosystems", ecosystems,
			"reason", "no runtime ecosystem maps to package sources",
		)
		return ListAutoProviders(), nil
	default:
		log.Debug(
			"routing: package provider selection is empty",
			"ecosystems", ecosystems,
			"reason", "runtime ecosystems carry no package sources",
		)
		return []upstream.PackageSource{}, nil
	}
}

func resolveExplicitSource(src types.SourceId) (
	[]upstream.PackageSource,
	error,
) {
	provider, ok := providerCatalogInstance().packageSources[src]
	if !ok {
		log.Debug(
			"routing: explicit package source rejected",
			"source", src,
			"reason", "source has no package provider",
		)
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedSource, src)
	}
	log.Debug(
		"routing: explicit package source selected",
		"source", src,
		"provider", provider.Id(),
	)
	return []upstream.PackageSource{provider}, nil
}

func validateSearchSourceEcosystem(
	src types.SourceId,
	platform types.Ecosystem,
) error {
	if !platform.IsSearchEcosystem() {
		return nil
	}

	support, ok := EcosystemSupportedBy(src, platform)
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
		provider, ok := providerCatalogInstance().packageSources[source]
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
		provider, ok := providerCatalogInstance().searchSources[source]
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
		provider, ok := providerCatalogInstance().infoSources[source]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedSource, source)
		}
		providers = append(providers, provider)
	}
	return providers, nil
}

func curseforgeAvailable() bool {
	_, ok := providerCatalogInstance().packageSources[types.SourceCurseForge]
	return ok
}
