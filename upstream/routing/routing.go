// Package routing composes configured provider instances with provider
// selection and execution policy.
//
// Responsibilities:
//   - Resolve ProviderUnspecified against an Ecosystem into ordered provider
//     candidates.
//   - Map an explicit Provider (or Registry for search) to exactly one
//     capability when supported.
//   - Route search, project metadata, package catalogues, and server products.
//   - Return typed selection errors for invalid/unsupported inputs.
//
// Non-responsibilities:
//   - Do not aggregate or merge upstream result payloads.
package routing

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/cache"
	catalogpkg "github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/internal/providers/curseforge"
	"github.com/mclucy/lucy/upstream/internal/providers/fabric"
	"github.com/mclucy/lucy/upstream/internal/providers/forge"
	"github.com/mclucy/lucy/upstream/internal/providers/hangar"
	"github.com/mclucy/lucy/upstream/internal/providers/mcdr"
	"github.com/mclucy/lucy/upstream/internal/providers/modrinth"
	"github.com/mclucy/lucy/upstream/internal/providers/mojang"
	"github.com/mclucy/lucy/upstream/internal/providers/neoforge"
	"github.com/mclucy/lucy/upstream/internal/providers/spiget"
)

var (
	ErrUnknownProvider     = errors.New("unknown provider")
	ErrUnsupportedProvider = errors.New("unsupported provider")
	ErrInvalidEcosystem    = errors.New("cannot find providers for ecosystem")
	ErrNoCatalogue         = errors.New("registry has no package catalogue")
)

type providerCatalog struct {
	searchProviders map[catalogpkg.Registry]upstream.SearchProvider
	infoProviders   map[catalogpkg.Provider]upstream.InfoProvider
	artifactMappers map[catalogpkg.Provider]upstream.ArtifactMapProvider
	catalogues      map[catalogpkg.Provider]upstream.PackageCatalogue
}

// Router binds configured provider instances to their capabilities and applies
// provider selection policy. One Router belongs to one invocation: providers
// own the network cache, logger, and API keys they were constructed with.
type Router struct {
	logger  *log.Logger
	network *cache.Client

	providers providerCatalog

	mojang   *mojang.Provider
	fabric   *fabric.Provider
	forge    *forge.Provider
	neoforge *neoforge.Provider
}

// New constructs a router over provider instances. network carries HTTP and
// cache configuration for metadata and artifact requests; logger may be nil.
// curseforgeAPIKey is the effective key: an empty key leaves CurseForge
// unregistered. Construction performs no network requests and decodes no key.
func New(
	network *cache.Client,
	logger *log.Logger,
	curseforgeAPIKey string,
) *Router {
	modrinthProvider := modrinth.New(network, logger)
	hangarProvider := hangar.New(network, logger)
	spigetProvider := spiget.New(network, logger)
	mcdrProvider := mcdr.New(network, logger)

	router := &Router{
		logger:   logger,
		network:  network,
		mojang:   mojang.New(network),
		fabric:   fabric.New(network),
		forge:    forge.New(network),
		neoforge: neoforge.New(network),
	}
	router.providers = providerCatalog{
		searchProviders: map[catalogpkg.Registry]upstream.SearchProvider{
			catalogpkg.RegistryModrinth: modrinthProvider,
			catalogpkg.RegistryMCDR:     mcdrProvider,
			catalogpkg.RegistryHangar:   hangarProvider,
			catalogpkg.RegistrySpiget:   spigetProvider,
		},
		infoProviders: map[catalogpkg.Provider]upstream.InfoProvider{
			catalogpkg.ProviderModrinth: modrinthProvider,
			catalogpkg.ProviderMCDR:     mcdrProvider,
			catalogpkg.ProviderHangar:   hangarProvider,
			catalogpkg.ProviderSpiget:   spigetProvider,
		},
		catalogues: map[catalogpkg.Provider]upstream.PackageCatalogue{
			catalogpkg.ProviderModrinth: modrinthProvider,
			catalogpkg.ProviderMCDR:     mcdrProvider,
			catalogpkg.ProviderHangar:   hangarProvider,
			catalogpkg.ProviderSpiget:   spigetProvider,
		},
		artifactMappers: map[catalogpkg.Provider]upstream.ArtifactMapProvider{
			catalogpkg.ProviderModrinth: modrinthProvider,
		},
	}
	if key := strings.TrimSpace(curseforgeAPIKey); key != "" {
		curseforgeProvider := curseforge.New(network, logger, key)
		router.providers.searchProviders[catalogpkg.RegistryCurseForge] = curseforgeProvider
		router.providers.infoProviders[catalogpkg.ProviderCurseForge] = curseforgeProvider
		router.providers.catalogues[catalogpkg.ProviderCurseForge] = curseforgeProvider
		router.providers.artifactMappers[catalogpkg.ProviderCurseForge] = curseforgeProvider
	}
	return router
}

func (r *Router) curseforgeAvailable() bool {
	_, ok := r.providers.catalogues[catalogpkg.ProviderCurseForge]
	return ok
}

// GetArtifactMapper returns the hash mapper registered for provider.
func (r *Router) GetArtifactMapper(
	provider catalogpkg.Provider,
) (upstream.ArtifactMapProvider, bool, error) {
	mapper, ok := r.providers.artifactMappers[provider]
	if !ok {
		r.logger.Debug(
			"routing: no artifact mapper for provider",
			"provider", provider,
			"reason", "provider has no artifact mapper",
		)
	}
	return mapper, ok, nil
}

func (r *Router) artifactMappers() []upstream.ArtifactMapProvider {
	providers := modProviders(r.curseforgeAvailable())
	mappers := make([]upstream.ArtifactMapProvider, 0, len(providers))
	for _, provider := range providers {
		mapper, ok := r.providers.artifactMappers[provider]
		if ok {
			mappers = append(mappers, mapper)
		}
	}
	return mappers
}

// ResolveSearchProviders resolves search providers for the platform. With a
// platform filter active, explicit registry selection is validated against
// registry capability data.
func (r *Router) ResolveSearchProviders(
	platform catalogpkg.Ecosystem,
	registry catalogpkg.Registry,
) ([]upstream.SearchProvider, error) {
	if registry == catalogpkg.RegistryUnknown {
		return nil, ErrUnknownProvider
	}

	if registry != catalogpkg.RegistryUnspecified {
		if err := validateSearchRegistryEcosystem(registry, platform); err != nil {
			r.logger.Debug(
				"routing: search registry rejected for platform",
				"registry", registry,
				"platform", platform,
				"reason", "registry does not support platform",
			)
			return nil, err
		}
		return r.resolveExplicitRegistry(registry)
	}

	keys := providersForEcosystem(platform, r.curseforgeAvailable())
	if len(keys) == 0 {
		r.logger.Debug(
			"routing: search providers unavailable for platform",
			"platform", platform,
			"reason", "no provider supports platform search",
		)
		return nil, fmt.Errorf("%w: %s", ErrInvalidEcosystem, platform)
	}
	providers, err := r.searchersFromKeys(keys)
	if err != nil {
		return nil, err
	}
	r.logger.Debug(
		"routing: search providers selected for platform",
		"platform", platform,
		"providers", keys,
	)
	return providers, nil
}

// ResolveInfoProviders resolves info providers for the platform and
// user-specified provider.
func (r *Router) ResolveInfoProviders(
	platform catalogpkg.Ecosystem,
	provider catalogpkg.Provider,
) ([]upstream.InfoProvider, error) {
	if provider == catalogpkg.ProviderUnknown {
		return nil, ErrUnknownProvider
	}

	if provider != catalogpkg.ProviderUnspecified {
		return r.resolveExplicitInformer(provider)
	}

	keys := providersForEcosystem(platform, r.curseforgeAvailable())
	if len(keys) == 0 {
		r.logger.Debug(
			"routing: info providers unavailable for ecosystem",
			"platform", platform,
			"reason", "unsupported ecosystem",
		)
		return nil, fmt.Errorf("%w: %s", ErrInvalidEcosystem, platform)
	}
	providers, err := r.informersFromKeys(keys)
	if err != nil {
		return nil, err
	}
	r.logger.Debug(
		"routing: info providers selected from ecosystem candidates",
		"platform", platform,
		"providers", keys,
	)
	return providers, nil
}

func (r *Router) resolveExplicitInformer(provider catalogpkg.Provider) (
	[]upstream.InfoProvider,
	error,
) {
	informer, ok := r.providers.infoProviders[provider]
	if !ok {
		r.logger.Debug(
			"routing: explicit info provider rejected",
			"provider", provider,
			"reason", "provider has no info capability",
		)
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
	r.logger.Debug(
		"routing: explicit info provider selected",
		"provider", provider,
		"capability", informer.Provider(),
	)
	return []upstream.InfoProvider{informer}, nil
}

func (r *Router) resolveExplicitRegistry(registry catalogpkg.Registry) (
	[]upstream.SearchProvider,
	error,
) {
	searcher, ok := r.providers.searchProviders[registry]
	if !ok {
		r.logger.Debug(
			"routing: explicit search registry rejected",
			"registry", registry,
			"reason", "registry has no search capability",
		)
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, registry)
	}
	r.logger.Debug(
		"routing: explicit search registry selected",
		"registry", registry,
		"provider", searcher.Provider(),
	)
	return []upstream.SearchProvider{searcher}, nil
}

func validateSearchRegistryEcosystem(
	registry catalogpkg.Registry,
	platform catalogpkg.Ecosystem,
) error {
	if !platform.IsSearchable() {
		return nil
	}

	provider := registry.Provider()
	if EcosystemSupportedBy(provider, platform) {
		return nil
	}

	return fmt.Errorf(
		"registry %s does not support platform %s",
		registry,
		platform,
	)
}

func (r *Router) searchersFromKeys(keys []catalogpkg.Provider) (
	[]upstream.SearchProvider,
	error,
) {
	capabilities := make([]upstream.SearchProvider, 0, len(keys))
	for _, key := range keys {
		registry, ok := key.Registry()
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, key)
		}
		capability, ok := r.providers.searchProviders[registry]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, key)
		}
		capabilities = append(capabilities, capability)
	}
	return capabilities, nil
}

func (r *Router) informersFromKeys(keys []catalogpkg.Provider) (
	[]upstream.InfoProvider,
	error,
) {
	capabilities := make([]upstream.InfoProvider, 0, len(keys))
	for _, key := range keys {
		capability, ok := r.providers.infoProviders[key]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, key)
		}
		capabilities = append(capabilities, capability)
	}
	return capabilities, nil
}
