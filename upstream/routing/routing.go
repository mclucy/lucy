// Package routing contains provider-to-capability bindings and provider
// resolution policies.
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
//   - Do not call provider APIs.
//   - Do not aggregate or merge upstream result payloads.
package routing

import (
	"errors"
	"fmt"
	"sync"

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

var providerCatalogOnce = sync.OnceValue(newProviderCatalog)

func newProviderCatalog() providerCatalog {
	catalog := providerCatalog{
		packageProviders: map[catalogpkg.Provider]upstream.PackageProvider{
			catalogpkg.ProviderModrinth: modrinth.Provider,
			catalogpkg.ProviderMCDR:     mcdr.Provider,
			catalogpkg.ProviderHangar:   hangar.Provider,
			catalogpkg.ProviderSpiget:   spiget.Provider,
		},
		searchProviders: map[catalogpkg.Registry]upstream.SearchProvider{
			catalogpkg.RegistryModrinth: modrinth.Provider,
			catalogpkg.RegistryMCDR:     mcdr.Provider,
			catalogpkg.RegistryHangar:   hangar.Provider,
			catalogpkg.RegistrySpiget:   spiget.Provider,
		},
		infoProviders: map[catalogpkg.Provider]upstream.InfoProvider{
			catalogpkg.ProviderModrinth: modrinth.Provider,
			catalogpkg.ProviderMCDR:     mcdr.Provider,
			catalogpkg.ProviderHangar:   hangar.Provider,
			catalogpkg.ProviderSpiget:   spiget.Provider,
		},
		artifactMappers: map[catalogpkg.Provider]upstream.ArtifactMapProvider{
			catalogpkg.ProviderModrinth: modrinth.Provider,
		},
		installerProviders: map[catalogpkg.Provider]upstream.InstallerProvider{
			catalogpkg.ProviderBMCLAPI:  bmclapi.Provider,
			catalogpkg.ProviderFabric:   fabric.Provider,
			catalogpkg.ProviderForge:    forge.Provider,
			catalogpkg.ProviderMojang:   mojang.Provider,
			catalogpkg.ProviderNeoForge: neoforge.Provider,
		},
	}
	if !curseforge.Enabled() {
		return catalog
	}

	catalog.packageProviders[catalogpkg.ProviderCurseForge] = curseforge.Provider
	catalog.searchProviders[catalogpkg.RegistryCurseForge] = curseforge.Provider
	catalog.infoProviders[catalogpkg.ProviderCurseForge] = curseforge.Provider
	catalog.artifactMappers[catalogpkg.ProviderCurseForge] = curseforge.Provider
	return catalog
}

func providerCatalogInstance() providerCatalog {
	return providerCatalogOnce()
}

// ListAutoProviders returns the default ordered provider list used when the
// provider is unspecified and the platform is unspecified.
func ListAutoProviders() []upstream.PackageProvider {
	capabilities, _ := packageProvidersFromKeys(autoProviders())
	return capabilities
}

func GetArtifactMapper(
	provider catalogpkg.Provider,
) (upstream.ArtifactMapProvider, bool, error) {
	mapper, ok := providerCatalogInstance().artifactMappers[provider]
	if !ok {
		log.Debug(
			"routing: no artifact mapper for provider",
			"provider", provider,
			"reason", "provider has no artifact mapper",
		)
	}
	return mapper, ok, nil
}

func artifactMappers() []upstream.ArtifactMapProvider {
	return artifactMappersFromProviders(modProviders())
}

func artifactMappersFromProviders(
	providers []catalogpkg.Provider,
) []upstream.ArtifactMapProvider {
	catalog := providerCatalogInstance()
	mappers := make([]upstream.ArtifactMapProvider, 0, len(providers))
	for _, provider := range providers {
		mapper, ok := catalog.artifactMappers[provider]
		if ok {
			mappers = append(mappers, mapper)
		}
	}
	return mappers
}

// InstallerFor returns the installer provider that can bootstrap the
// ecosystem.
func InstallerFor(ecosystem catalogpkg.Ecosystem) (
	upstream.InstallerProvider,
	bool,
) {
	provider, ok := installerProviderForEcosystem(ecosystem)
	if !ok {
		return nil, false
	}
	return InstallerForProvider(provider)
}

// InstallerForProvider returns the installer provider registered for the
// provider key.
func InstallerForProvider(provider catalogpkg.Provider) (
	upstream.InstallerProvider,
	bool,
) {
	installer, ok := providerCatalogInstance().installerProviders[provider]
	if !ok {
		log.Debug(
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
		log.Debug(
			"routing: no installer provider",
			"ecosystem", ecosystem,
			"reason", "unsupported ecosystem",
		)
		return catalogpkg.ProviderUnknown, false
	}
}

// ResolveProviders resolves ordered provider candidates for the platform and
// user-specified provider.
func ResolveProviders(
	platform catalogpkg.Ecosystem,
	provider catalogpkg.Provider,
) ([]upstream.PackageProvider, error) {
	if provider == catalogpkg.ProviderUnknown {
		return nil, ErrUnknownProvider
	}

	if provider != catalogpkg.ProviderUnspecified {
		return resolveExplicitProvider(provider)
	}

	keys, err := providersForEcosystem(platform)
	if err != nil {
		log.Debug(
			"routing: package providers unavailable for ecosystem",
			"platform", platform,
			"reason", "unsupported ecosystem",
		)
		return nil, fmt.Errorf("%w: %s", err, platform)
	}
	providers, err := packageProvidersFromKeys(keys)
	if err != nil {
		return nil, err
	}
	log.Debug(
		"routing: package providers selected from ecosystem candidates",
		"platform", platform,
		"providers", keys,
	)
	return providers, nil
}

// ResolveSearchProviders resolves search providers for the platform. With a
// platform filter active, explicit registry selection is validated against
// registry capability data.
func ResolveSearchProviders(
	platform catalogpkg.Ecosystem,
	registry catalogpkg.Registry,
) ([]upstream.SearchProvider, error) {
	if registry == catalogpkg.RegistryUnknown {
		return nil, ErrUnknownProvider
	}

	if registry != catalogpkg.RegistryUnspecified {
		if err := validateSearchRegistryEcosystem(registry, platform); err != nil {
			log.Debug(
				"routing: search registry rejected for platform",
				"registry", registry,
				"platform", platform,
				"reason", "registry does not support platform",
			)
			return nil, err
		}
		return resolveExplicitRegistry(registry)
	}

	if !platform.IsSearchable() {
		keys, err := providersForEcosystem(platform)
		if err != nil {
			log.Debug(
				"routing: search providers unavailable for ecosystem",
				"platform", platform,
				"reason", "unsupported ecosystem",
			)
			return nil, fmt.Errorf("%w: %s", err, platform)
		}
		providers, err := searchersFromKeys(keys)
		if err != nil {
			return nil, err
		}
		log.Debug(
			"routing: search providers selected from ecosystem candidates",
			"platform", platform,
			"providers", keys,
		)
		return providers, nil
	}

	keys := searchProvidersForEcosystem(platform)
	if len(keys) == 0 {
		log.Debug(
			"routing: search providers unavailable for platform",
			"platform", platform,
			"reason", "no provider supports platform search",
		)
		return nil, fmt.Errorf("%w: %s", ErrInvalidEcosystem, platform)
	}
	providers, err := searchersFromKeys(keys)
	if err != nil {
		return nil, err
	}
	log.Debug(
		"routing: search providers selected for platform",
		"platform", platform,
		"providers", keys,
	)
	return providers, nil
}

func ResolveInfoProviders(
	platform catalogpkg.Ecosystem,
	provider catalogpkg.Provider,
) ([]upstream.InfoProvider, error) {
	if provider == catalogpkg.ProviderUnknown {
		return nil, ErrUnknownProvider
	}

	if provider != catalogpkg.ProviderUnspecified {
		return resolveExplicitInformer(provider)
	}

	keys, err := providersForEcosystem(platform)
	if err != nil {
		log.Debug(
			"routing: info providers unavailable for ecosystem",
			"platform", platform,
			"reason", "unsupported ecosystem",
		)
		return nil, fmt.Errorf("%w: %s", err, platform)
	}
	providers, err := informersFromKeys(keys)
	if err != nil {
		return nil, err
	}
	log.Debug(
		"routing: info providers selected from ecosystem candidates",
		"platform", platform,
		"providers", keys,
	)
	return providers, nil
}

func resolveExplicitInformer(provider catalogpkg.Provider) (
	[]upstream.InfoProvider,
	error,
) {
	informer, ok := providerCatalogInstance().infoProviders[provider]
	if !ok {
		log.Debug(
			"routing: explicit info provider rejected",
			"provider", provider,
			"reason", "provider has no info capability",
		)
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
	log.Debug(
		"routing: explicit info provider selected",
		"provider", provider,
		"capability", informer.Provider(),
	)
	return []upstream.InfoProvider{informer}, nil
}

func resolveExplicitRegistry(registry catalogpkg.Registry) (
	[]upstream.SearchProvider,
	error,
) {
	searcher, ok := providerCatalogInstance().searchProviders[registry]
	if !ok {
		log.Debug(
			"routing: explicit search registry rejected",
			"registry", registry,
			"reason", "registry has no search capability",
		)
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, registry)
	}
	log.Debug(
		"routing: explicit search registry selected",
		"registry", registry,
		"provider", searcher.Provider(),
	)
	return []upstream.SearchProvider{searcher}, nil
}

func ResolveProvidersForRuntime(
	ecosystems []catalogpkg.Ecosystem,
	provider catalogpkg.Provider,
) ([]upstream.PackageProvider, error) {
	if provider == catalogpkg.ProviderUnknown {
		return nil, ErrUnknownProvider
	}

	if provider != catalogpkg.ProviderUnspecified {
		return resolveExplicitProvider(provider)
	}

	if len(ecosystems) == 0 {
		log.Debug(
			"routing: package providers unavailable for runtime",
			"reason", "no runtime ecosystems",
		)
		return nil, fmt.Errorf("routing: runtime ecosystems unavailable, cannot resolve providers")
	}

	selection := providersFromEcosystems(ecosystems)
	switch {
	case len(selection.providers) > 0:
		log.Debug(
			"routing: package providers selected from runtime ecosystems",
			"ecosystems", ecosystems,
			"providers", selection.providers,
		)
		return packageProvidersFromKeys(selection.providers)
	case selection.fallback:
		log.Debug(
			"routing: package providers fell back to auto list",
			"ecosystems", ecosystems,
			"reason", "no runtime ecosystem maps to package providers",
		)
		return ListAutoProviders(), nil
	default:
		log.Debug(
			"routing: package provider selection is empty",
			"ecosystems", ecosystems,
			"reason", "runtime ecosystems carry no package providers",
		)
		return []upstream.PackageProvider{}, nil
	}
}

func resolveExplicitProvider(provider catalogpkg.Provider) (
	[]upstream.PackageProvider,
	error,
) {
	capability, ok := providerCatalogInstance().packageProviders[provider]
	if !ok {
		log.Debug(
			"routing: explicit package provider rejected",
			"provider", provider,
			"reason", "provider has no package capability",
		)
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
	log.Debug(
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

func packageProvidersFromKeys(providers []catalogpkg.Provider) (
	[]upstream.PackageProvider,
	error,
) {
	capabilities := make([]upstream.PackageProvider, 0, len(providers))
	for _, provider := range providers {
		capability, ok := providerCatalogInstance().packageProviders[provider]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
		}
		capabilities = append(capabilities, capability)
	}
	return capabilities, nil
}

func searchersFromKeys(keys []catalogpkg.Provider) (
	[]upstream.SearchProvider,
	error,
) {
	capabilities := make([]upstream.SearchProvider, 0, len(keys))
	for _, key := range keys {
		registry, ok := key.Registry()
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, key)
		}
		capability, ok := providerCatalogInstance().searchProviders[registry]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, key)
		}
		capabilities = append(capabilities, capability)
	}
	return capabilities, nil
}

func informersFromKeys(keys []catalogpkg.Provider) (
	[]upstream.InfoProvider,
	error,
) {
	capabilities := make([]upstream.InfoProvider, 0, len(keys))
	for _, key := range keys {
		capability, ok := providerCatalogInstance().infoProviders[key]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, key)
		}
		capabilities = append(capabilities, capability)
	}
	return capabilities, nil
}

func curseforgeAvailable() bool {
	_, ok := providerCatalogInstance().packageProviders[catalogpkg.ProviderCurseForge]
	return ok
}
