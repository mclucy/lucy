package install

import (
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/workspace"
)

type InstallOptions struct {
	WithOptional bool
	Force        bool
	Journal      Journal
	Workspace    func() workspace.Workspace
	Cache        func(
		url, destDir string,
		opts cache.DownloadOptions,
	) (*cache.DownloadResult, error)
	Providers func(*workspace.ServerInstance) []upstream.PackageSource
}

func DefaultOptions() InstallOptions {
	return InstallOptions{}
}

func (o InstallOptions) withDefaults() InstallOptions {
	if o.Journal == nil {
		o.Journal = logJournal{}
	}
	if o.Workspace == nil {
		o.Workspace = workspace.New
	}
	if o.Cache == nil {
		o.Cache = cache.CachedDownload
	}
	if o.Providers == nil {
		o.Providers = func(server *workspace.ServerInstance) []upstream.PackageSource {
			if server == nil {
				return nil
			}
			providers, err := routing.ResolveProvidersForRuntime(
				effectiveRuntimeEcosystems(server),
				types.SourceAuto,
			)
			if err != nil {
				return nil
			}
			return providers
		}
	}
	return o
}

func effectiveRuntimeEcosystems(
	server *workspace.ServerInstance,
) []types.Ecosystem {
	if server == nil {
		return nil
	}
	offers := server.EffectiveEcosystems()
	ecosystems := make([]types.Ecosystem, 0, len(offers))
	for _, offer := range offers {
		ecosystems = append(ecosystems, offer.Ecosystem)
	}
	return ecosystems
}

func defaultRegularEcosystem(
	server *workspace.ServerInstance,
) types.Ecosystem {
	if server == nil {
		return types.EcoUnspecified
	}
	if loader := server.DerivedModLoader(); loader.IsModding() {
		return loader
	}
	for _, offer := range server.EffectiveEcosystems() {
		if offer.Compatibility == types.CompatCompatible &&
			offer.Ecosystem.IsSearchEcosystem() {
			return offer.Ecosystem
		}
	}
	return types.EcoUnspecified
}
