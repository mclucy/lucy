package hangar

import (
	"context"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/upstream"
)

// Provider resolves Hangar projects and versions through one configured
// network cache. logger may be nil, which records nothing.
type Provider struct {
	network *cache.Client
	logger  *log.Logger
}

// New constructs a Hangar provider. network supplies the cached HTTP
// transport; construction performs no requests.
func New(network *cache.Client, logger *log.Logger) *Provider {
	return &Provider{network: network, logger: logger}
}

func (p *Provider) Provider() catalog.Provider {
	return catalog.ProviderHangar
}

// Builds lists every published version. Hangar versions declare supported
// platforms independently, so each platform becomes its own build entry.
func (p *Provider) Builds(ctx context.Context, project string) ([]upstream.Build, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	versions, err := p.listVersions(ctx, catalog.Name(project))
	if err != nil {
		return nil, err
	}

	var builds []upstream.Build
	for i := range versions {
		builds = append(builds, versions[i].ToBuilds()...)
	}
	return builds, nil
}

func (p *Provider) Search(ctx context.Context, q upstream.Query) (upstream.SearchResponse, error) {
	// Hangar publishes server plugins only; IncludeClient needs no filter.
	res, err := p.searchProjects(ctx, q.Keyword, q.FilterEcosystem)
	if err != nil {
		return upstream.SearchResponse{}, err
	}
	return res.ToSearchResults(p.Provider()), nil
}

func (p *Provider) Info(ctx context.Context, ref catalog.Ref) (catalog.Metadata, error) {
	project, err := p.getProject(ctx, ref.Name)
	if err != nil {
		return catalog.Metadata{}, err
	}
	info := project.ToProjectInformation()
	return info, nil
}
