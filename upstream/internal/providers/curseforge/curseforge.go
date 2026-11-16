// Package curseforge provides functions to interact with CurseForge API.
//
// CurseForge identifies mods by numeric modId, not by slug. Slug resolution
// is done via the search endpoint with the slug query parameter.
//
// All API requests require an x-api-key header. The key is supplied once to
// New and never read from ambient global state.
package curseforge

import (
	"context"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/upstream"
)

// Provider is one configured CurseForge API client. Every request it performs
// shares the network cache, logger, and API key it was constructed with.
type Provider struct {
	network *cache.Client
	logger  *log.Logger
	apiKey  string
}

// New constructs a CurseForge provider over network, logger, and apiKey.
// Construction performs no network requests and decodes no key.
func New(
	network *cache.Client,
	logger *log.Logger,
	apiKey string,
) *Provider {
	return &Provider{
		network: network,
		logger:  logger,
		apiKey:  strings.TrimSpace(apiKey),
	}
}

func (p *Provider) Provider() catalog.Provider {
	return catalog.ProviderCurseForge
}

func (p *Provider) Search(
	ctx context.Context,
	q upstream.Query,
) (upstream.SearchResponse, error) {
	// CurseForge exposes no client/server search facet; IncludeClient cannot
	// be applied upstream and is left to post-filtering. An ecosystem that
	// accepts more than one loader (Quilt accepts Fabric) needs one request
	// per loader filter, merged by project identity.
	search := searchResponse{}
	seen := make(map[int32]bool)
	for _, loaderType := range modLoaderTypes(q.FilterEcosystem) {
		u := searchUrlForLoader(catalog.Name(q.Keyword), loaderType)
		p.logger.Debug("searching via curseforge api: " + u)

		var resp searchResponse
		if err := p.get(ctx, u, &resp); err != nil {
			return upstream.SearchResponse{}, err
		}
		for _, mod := range resp.Data {
			if seen[mod.ID] {
				continue
			}
			seen[mod.ID] = true
			search.Data = append(search.Data, mod)
		}
	}
	return search.ToSearchResults(p.Provider()), nil
}

func (p *Provider) Info(
	ctx context.Context,
	ref catalog.Ref,
) (catalog.Metadata, error) {
	mod, err := p.resolveSlug(ctx, ref.Name)
	if err != nil {
		return catalog.Metadata{}, err
	}
	description, err := p.getModDescription(ctx, mod.ID)
	if err != nil {
		return catalog.Metadata{}, err
	}
	info := rawProjectInformation{
		mod: mod, description: description,
	}.ToProjectInformation()
	return info, nil
}
