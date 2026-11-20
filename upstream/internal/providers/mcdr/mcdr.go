package mcdr

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/github"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/upstream"
)

// Provider resolves MCDR plugin catalogue entries through the official
// catalogue API and, for search ids, GitHub. logger may be nil, which records
// nothing.
type Provider struct {
	network      *cache.Client
	logger       *log.Logger
	githubClient *github.Client
}

// New constructs an MCDR provider. network supplies the cached HTTP transport;
// construction performs no requests.
func New(network *cache.Client, logger *log.Logger) *Provider {
	return &Provider{
		network:      network,
		logger:       logger,
		githubClient: github.New(network, logger),
	}
}

func (p *Provider) Provider() catalog.Provider {
	return catalog.ProviderMCDR
}

// Builds lists every release in the plugin catalogue release history.
func (p *Provider) Builds(ctx context.Context, project string) ([]upstream.Build, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	history, err := p.getReleaseHistory(ctx, catalog.Name(project).PythonName())
	if err != nil {
		return nil, err
	}

	builds := make([]upstream.Build, 0, len(history.Releases))
	for _, rel := range history.Releases {
		builds = append(builds, rel.ToBuild())
	}
	return builds, nil
}

type mcdrSearchResult []string

func (m mcdrSearchResult) ToSearchResults(source catalog.Provider) upstream.SearchResponse {
	res := upstream.SearchResponse{Provider: source}
	for _, id := range m {
		res.Items = append(
			res.Items, upstream.SearchResult{
				ProjectName: input.ToProjectName(id).String(),
				Provider:    source,
			},
		)
	}
	return res
}

// Search returns the provider's default result set; tags and limit are not
// applied.

func (p *Provider) Search(ctx context.Context, q upstream.Query) (upstream.SearchResponse, error) {
	// MCDR publishes server plugins only; IncludeClient needs no filter.
	if q.FilterEcosystem != catalog.EcosystemMCDR && q.FilterEcosystem != catalog.EcosystemUnspecified {
		return upstream.SearchResponse{}, fmt.Errorf(
			"invalid search platform: expected %s, got %s",
			catalog.EcosystemMCDR,
			q.FilterEcosystem,
		)
	}
	res, err := p.search(ctx, q.Keyword)
	if err != nil {
		return upstream.SearchResponse{}, err
	}
	return res.ToSearchResults(p.Provider()), nil
}

// Info resolves plugin metadata from the catalogue's per-plugin aggregate
// document in one request, plus the shared author index when the plugin lists
// authors. Sections the catalogue marks null stay absent.
func (p *Provider) Info(ctx context.Context, ref catalog.Ref) (catalog.Metadata, error) {
	name := ref.Name.PythonName()
	all, err := p.getPlugin(ctx, name)
	if err != nil {
		return catalog.Metadata{}, err
	}

	var authorLinks map[string]string
	if all.Plugin != nil && len(all.Plugin.Authors) > 0 {
		authorLinks, err = p.getAuthorLinks(ctx)
		if err != nil {
			return catalog.Metadata{}, err
		}
	}

	info := rawProjectInformation{
		Info:        all.Plugin,
		Meta:        all.Meta,
		Repository:  all.Repository,
		AuthorLinks: authorLinks,
	}.ToProjectInformation()
	return info, nil
}
