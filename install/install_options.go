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
	Providers func(workspace.Workspace) []upstream.PackageSource
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
		o.Providers = func(ws workspace.Workspace) []upstream.PackageSource {
			providers, err := routing.ResolveProvidersForRuntime(
				effectiveRuntimeEcosystems(ws),
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
	ws workspace.Workspace,
) []types.Ecosystem {
	offers := ws.EffectiveEcosystems()
	ecosystems := make([]types.Ecosystem, 0, len(offers))
	for _, offer := range offers {
		ecosystems = append(ecosystems, offer.Ecosystem)
	}
	return ecosystems
}

func defaultRegularEcosystem(
	ws workspace.Workspace,
) types.Ecosystem {
	server := ws.Server()
	if server == nil {
		return types.EcoUnspecified
	}
	if loader := server.ModLoader(); loader.IsModding() {
		return loader
	}
	for _, offer := range ws.EffectiveEcosystems() {
		if offer.Compatibility == types.CompatFull &&
			offer.Ecosystem.IsSearchEcosystem() {
			return offer.Ecosystem
		}
	}
	return types.EcoUnspecified
}
