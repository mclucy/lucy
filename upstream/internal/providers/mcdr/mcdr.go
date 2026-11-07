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

// Provider resolves MCDR plugin catalogue entries through one configured
// network cache and GitHub client. logger may be nil, which records nothing.
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

func (p *Provider) Info(ctx context.Context, ref catalog.Ref) (catalog.Metadata, error) {
	name := ref.Name.PythonName()
	plugin, err := p.getInfo(ctx, name)
	if err != nil {
		return catalog.Metadata{}, err
	}
	meta, err := p.getMeta(ctx, name)
	if err != nil {
		return catalog.Metadata{}, err
	}
	repo, err := p.getRepository(ctx, name)
	if err != nil {
		return catalog.Metadata{}, err
	}

	info := rawProjectInformation{
		Info:       plugin,
		Meta:       meta,
		Repository: repo,
	}.ToProjectInformation()
	return info, nil
}
