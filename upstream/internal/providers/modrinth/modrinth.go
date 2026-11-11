// Package modrinth provides functions to interact with Modrinth API.
//
// Terminology: a Modrinth project maps to a Lucy project, and a Modrinth
// version maps to a Lucy package version. Lucy-side names are Modrinth slugs;
// private helpers say "slug" for the project and "version" for a release,
// beta, or alpha.
package modrinth

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/upstream"
)

// Provider is one configured Modrinth API client. Every request it performs
// shares the network cache and logger it was constructed with.
type Provider struct {
	network *cache.Client
	logger  *log.Logger
}

// New constructs a Modrinth provider over network and logger. Construction
// performs no network requests.
func New(network *cache.Client, logger *log.Logger) *Provider {
	return &Provider{network: network, logger: logger}
}

func (p *Provider) Provider() catalog.Provider {
	return catalog.ProviderModrinth
}

func (p *Provider) Search(ctx context.Context, q upstream.Query) (
	resp upstream.SearchResponse,
	err error,
) {
	var facets []facetItems
	facets = append(facets, loaderFacets(q.FilterEcosystem))

	if !q.IncludeClient {
		facets = append(facets, facetServerSupported)
	}

	internalOptions := searchOptions{
		index:  "relevance",
		facets: facets,
	}
	searchUrl := searchUrl(q.Keyword, internalOptions)

	p.logger.Debug("searching via modrinth api: " + searchUrl)
	res, err := p.requestBytes(ctx, searchUrl)
	if err != nil {
		return resp, fmt.Errorf("modrinth: search request failed: %w", err)
	}
	if res.StatusCode != 200 {
		return resp, fmt.Errorf(
			"%w: status %d",
			ErrInvalidAPIResponse,
			res.StatusCode,
		)
	}
	result := &searchResultResponse{}
	err = json.Unmarshal(res.Data, result)
	if err != nil {
		return resp, err
	}

	items := make([]upstream.SearchResult, len(result.Hits))
	for i, hit := range result.Hits {
		items[i] = upstream.SearchResult{
			ProjectName: hit.Slug,
			Provider:    p.Provider(),
			Title:       hit.Title,
			Description: hit.Description,
			Downloads:   int64(hit.Downloads),
			LastUpdated: hit.DateModified,
		}
	}
	resp = upstream.SearchResponse{
		Provider: p.Provider(),
		Items:    items,
		Warnings: nil,
	}

	return
}

func (p *Provider) Info(
	ctx context.Context,
	ref catalog.Ref,
) (catalog.Metadata, error) {
	project, err := p.getProjectByName(ctx, ref.Name)
	if err != nil {
		return catalog.Metadata{}, err
	}
	info := project.ToProjectInformation()
	return info, nil
}

var ErrInvalidAPIResponse = errors.New("received non-200 code from modrinth api")
