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
	Providers func(types.RuntimeTopology) []upstream.PackageSource
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
		o.Providers = func(topology types.RuntimeTopology) []upstream.PackageSource {
			providers, err := routing.ResolveProvidersFromTopology(
				&topology,
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
