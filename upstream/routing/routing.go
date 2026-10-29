// Package routing composes configured provider instances with provider
// selection and execution policy.
//
// Responsibilities:
//   - Resolve ProviderUnspecified against an Ecosystem into ordered provider
//     candidates.
//   - Map an explicit Provider (or Registry for search) to exactly one
//     capability when supported.
//   - Apply operation-aware routing policy (search/info/fetch/dependencies).
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
	ErrUnknownProvider     = errors.New("unknown provider")
	ErrUnsupportedProvider = errors.New("unsupported provider")
	ErrInvalidEcosystem    = errors.New("cannot find providers for ecosystem")
	ErrNoCatalogue         = errors.New("registry has no package catalogue")
)

type providerCatalog struct {
	packageProviders   map[catalogpkg.Provider]upstream.PackageProvider
	searchProviders    map[catalogpkg.Registry]upstream.SearchProvider
	infoProviders      map[catalogpkg.Provider]upstream.InfoProvider
	artifactMappers    map[catalogpkg.Provider]upstream.ArtifactMapProvider
	installerProviders map[catalogpkg.Provider]upstream.InstallerProvider
}

// Router binds configured provider instances to their capabilities and applies
// provider selection policy. One Router belongs to one invocation: providers
// own the network cache, logger, and API keys they were constructed with.
type Router struct {
	logger  *log.Logger
	network *cache.Client

	providers providerCatalog

	modrinth   *modrinth.Provider
	curseforge *curseforge.Provider
	hangar     *hangar.Provider
	spiget     *spiget.Provider
	mcdr       *mcdr.Provider
	mojang     *mojang.Provider
	fabric     *fabric.Provider
	forge      *forge.Provider
	neoforge   *neoforge.Provider
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
	router := &Router{
		logger:   logger,
		network:  network,
		modrinth: modrinth.New(network, logger),
		hangar:   hangar.New(network, logger),
		spiget:   spiget.New(network, logger),
		mcdr:     mcdr.New(network, logger),
		mojang:   mojang.New(network),
		fabric:   fabric.New(network),
		forge:    forge.New(network),
		neoforge: neoforge.New(network),
	}
	router.providers = providerCatalog{
		packageProviders: map[catalogpkg.Provider]upstream.PackageProvider{
			catalogpkg.ProviderModrinth: router.modrinth,
			catalogpkg.ProviderMCDR:     router.mcdr,
			catalogpkg.ProviderHangar:   router.hangar,
			catalogpkg.ProviderSpiget:   router.spiget,
		},
		searchProviders: map[catalogpkg.Registry]upstream.SearchProvider{
			catalogpkg.RegistryModrinth: router.modrinth,
			catalogpkg.RegistryMCDR:     router.mcdr,
			catalogpkg.RegistryHangar:   router.hangar,
			catalogpkg.RegistrySpiget:   router.spiget,
		},
		infoProviders: map[catalogpkg.Provider]upstream.InfoProvider{
			catalogpkg.ProviderModrinth: router.modrinth,
			catalogpkg.ProviderMCDR:     router.mcdr,
			catalogpkg.ProviderHangar:   router.hangar,
			catalogpkg.ProviderSpiget:   router.spiget,
		},
		artifactMappers: map[catalogpkg.Provider]upstream.ArtifactMapProvider{
			catalogpkg.ProviderModrinth: router.modrinth,
		},
		installerProviders: map[catalogpkg.Provider]upstream.InstallerProvider{
			catalogpkg.ProviderBMCLAPI: bmclapi.New(
				router.mojang,
				router.forge,
				router.neoforge,
			),
			catalogpkg.ProviderFabric:   router.fabric,
			catalogpkg.ProviderForge:    router.forge,
			catalogpkg.ProviderMojang:   router.mojang,
			catalogpkg.ProviderNeoForge: router.neoforge,
		},
	}
	if key := strings.TrimSpace(curseforgeAPIKey); key != "" {
		router.curseforge = curseforge.New(network, logger, key)
		router.providers.packageProviders[catalogpkg.ProviderCurseForge] = router.curseforge
		router.providers.searchProviders[catalogpkg.RegistryCurseForge] = router.curseforge
		router.providers.infoProviders[catalogpkg.ProviderCurseForge] = router.curseforge
		router.providers.artifactMappers[catalogpkg.ProviderCurseForge] = router.curseforge
	}
	return router
}

func (r *Router) curseforgeAvailable() bool {
	return r.curseforge != nil
}

// ListAutoProviders returns the default ordered provider list used when the
// provider is unspecified and the platform is unspecified.
func (r *Router) ListAutoProviders() []upstream.PackageProvider {
	capabilities, _ := r.packageProvidersFromKeys(
		autoProviders(r.curseforgeAvailable()),
	)
	return capabilities
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

// InstallerFor returns the installer provider that can bootstrap the
// ecosystem.
func (r *Router) InstallerFor(ecosystem catalogpkg.Ecosystem) (
	upstream.InstallerProvider,
	bool,
) {
	provider, ok := installerProviderForEcosystem(ecosystem)
	if !ok {
		return nil, false
	}
	return r.InstallerForProvider(provider)
}

// InstallerForProvider returns the installer provider registered for the
// provider key.
func (r *Router) InstallerForProvider(provider catalogpkg.Provider) (
	upstream.InstallerProvider,
	bool,
) {
	installer, ok := r.providers.installerProviders[provider]
	if !ok {
		r.logger.Debug(
			"routing: no installer provider",
			"provider", provider,
			"reason", "provider has no installer capability",
		)
	}
	return installer, ok
}

func installerProviderForEcosystem(ecosystem catalogpkg.Ecosystem) (
	catalogpkg.Provider,
	bool,
) {
	switch ecosystem {
	case catalogpkg.EcosystemMinecraft:
		return catalogpkg.ProviderMojang, true
	case catalogpkg.EcosystemForge:
		return catalogpkg.ProviderForge, true
	case catalogpkg.EcosystemNeoForge:
		return catalogpkg.ProviderNeoForge, true
	case catalogpkg.EcosystemFabric:
		return catalogpkg.ProviderFabric, true
	default:
		return catalogpkg.ProviderUnknown, false
	}
}

// ResolveProviders resolves ordered provider candidates for the platform and
// user-specified provider.
func (r *Router) ResolveProviders(
	platform catalogpkg.Ecosystem,
	provider catalogpkg.Provider,
) ([]upstream.PackageProvider, error) {
	if provider == catalogpkg.ProviderUnknown {
		return nil, ErrUnknownProvider
	}

	if provider != catalogpkg.ProviderUnspecified {
		return r.resolveExplicitProvider(provider)
	}

	keys, err := providersForEcosystem(platform, r.curseforgeAvailable())
	if err != nil {
		r.logger.Debug(
			"routing: package providers unavailable for ecosystem",
			"platform", platform,
			"reason", "unsupported ecosystem",
		)
		return nil, fmt.Errorf("%w: %s", err, platform)
	}
	providers, err := r.packageProvidersFromKeys(keys)
	if err != nil {
		return nil, err
	}
	r.logger.Debug(
		"routing: package providers selected from ecosystem candidates",
		"platform", platform,
		"providers", keys,
	)
	return providers, nil
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

	if !platform.IsSearchable() {
		keys, err := providersForEcosystem(platform, r.curseforgeAvailable())
		if err != nil {
			r.logger.Debug(
				"routing: search providers unavailable for ecosystem",
				"platform", platform,
				"reason", "unsupported ecosystem",
			)
			return nil, fmt.Errorf("%w: %s", err, platform)
		}
		providers, err := r.searchersFromKeys(keys)
		if err != nil {
			return nil, err
		}
		r.logger.Debug(
			"routing: search providers selected from ecosystem candidates",
			"platform", platform,
			"providers", keys,
		)
		return providers, nil
	}

	keys := searchProvidersForEcosystem(platform, r.curseforgeAvailable())
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

	keys, err := providersForEcosystem(platform, r.curseforgeAvailable())
	if err != nil {
		r.logger.Debug(
			"routing: info providers unavailable for ecosystem",
			"platform", platform,
			"reason", "unsupported ecosystem",
		)
		return nil, fmt.Errorf("%w: %s", err, platform)
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

// ResolveProvidersForRuntime resolves package providers from the runtime's
// ecosystems and the user-specified provider.
func (r *Router) ResolveProvidersForRuntime(
	ecosystems []catalogpkg.Ecosystem,
	provider catalogpkg.Provider,
) ([]upstream.PackageProvider, error) {
	if provider == catalogpkg.ProviderUnknown {
		return nil, ErrUnknownProvider
	}

	if provider != catalogpkg.ProviderUnspecified {
		return r.resolveExplicitProvider(provider)
	}

	if len(ecosystems) == 0 {
		r.logger.Debug(
			"routing: package providers unavailable for runtime",
			"reason", "no runtime ecosystems",
		)
		return nil, fmt.Errorf("routing: runtime ecosystems unavailable, cannot resolve providers")
	}

	selection := providersFromEcosystems(ecosystems, r.curseforgeAvailable())
	switch {
	case len(selection.providers) > 0:
		r.logger.Debug(
			"routing: package providers selected from runtime ecosystems",
			"ecosystems", ecosystems,
			"providers", selection.providers,
		)
		return r.packageProvidersFromKeys(selection.providers)
	case selection.fallback:
		r.logger.Debug(
			"routing: package providers fell back to auto list",
			"ecosystems", ecosystems,
			"reason", "no runtime ecosystem maps to package providers",
		)
		return r.ListAutoProviders(), nil
	default:
		r.logger.Debug(
			"routing: package provider selection is empty",
			"ecosystems", ecosystems,
			"reason", "runtime ecosystems carry no package providers",
		)
		return []upstream.PackageProvider{}, nil
	}
}

func (r *Router) resolveExplicitProvider(provider catalogpkg.Provider) (
	[]upstream.PackageProvider,
	error,
) {
	capability, ok := r.providers.packageProviders[provider]
	if !ok {
		r.logger.Debug(
			"routing: explicit package provider rejected",
			"provider", provider,
			"reason", "provider has no package capability",
		)
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
	r.logger.Debug(
		"routing: explicit package provider selected",
		"provider", provider,
		"capability", capability.Provider(),
	)
	return []upstream.PackageProvider{capability}, nil
}

func validateSearchRegistryEcosystem(
	registry catalogpkg.Registry,
	platform catalogpkg.Ecosystem,
) error {
	if !platform.IsSearchable() {
		return nil
	}

	provider := registry.Provider()
	support, ok := EcosystemSupportedBy(provider, platform)
	if ok && support.Supported {
		return nil
	}

	return fmt.Errorf(
		"registry %s does not support platform %s",
		registry,
		platform,
	)
}

func (r *Router) packageProvidersFromKeys(providers []catalogpkg.Provider) (
	[]upstream.PackageProvider,
	error,
) {
	capabilities := make([]upstream.PackageProvider, 0, len(providers))
	for _, provider := range providers {
		capability, ok := r.providers.packageProviders[provider]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
		}
		capabilities = append(capabilities, capability)
	}
	return capabilities, nil
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
