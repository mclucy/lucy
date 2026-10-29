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
	"fmt"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
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
	// be applied upstream and is left to post-filtering.
	u := searchUrl(catalog.Name(q.Keyword), q.FilterEcosystem)
	p.logger.Debug("searching via curseforge api: " + u)

	resp := &searchResponse{}
	if err := p.get(ctx, u, resp); err != nil {
		return upstream.SearchResponse{}, err
	}
	return resp.ToSearchResults(p.Provider()), nil
}

func (p *Provider) Fetch(
	ctx context.Context,
	local upstream.LocalContext,
	id catalog.VersionedRef,
) (upstream.ResolvedPackage, error) {
	mod, err := p.resolveSlug(ctx, id.Name)
	if err != nil {
		return upstream.ResolvedPackage{}, err
	}

	file, err := p.getFileByDisplayName(
		ctx,
		mod.ID,
		string(id.Version),
		local,
		id.Ecosystem,
	)
	if err != nil {
		return upstream.ResolvedPackage{}, err
	}

	resolved := file.ToPackageRemote()
	resolved.ID.Ref = id.Ref
	resolved.ID.Version = id.Version
	return resolved, nil
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

func (p *Provider) Dependencies(
	ctx context.Context,
	local upstream.LocalContext,
	id catalog.VersionedRef,
) (*upstream.DependencySet, error) {
	mod, err := p.resolveSlug(ctx, id.Name)
	if err != nil {
		return nil, err
	}

	file, err := p.getFileByDisplayName(
		ctx,
		mod.ID,
		string(id.Version),
		local,
		id.Ecosystem,
	)
	if err != nil {
		return nil, err
	}

	return new((&curseforgeDependencies{file: file}).ToDependencySet()), nil
}

// curseforgeDependencies wraps a fileResponse for dependency
// normalization.
type curseforgeDependencies struct {
	file *fileResponse
}

func (c *curseforgeDependencies) ToDependencySet() upstream.DependencySet {
	result := upstream.DependencySet{
		FromArtifact: false,
	}

	for _, dep := range c.file.Dependencies {
		// relationType mapping:
		// 1 = EmbeddedLibrary (skip - embedded in the mod itself)
		// 2 = OptionalDependency -> Mandatory: false
		// 3 = RequiredDependency -> Mandatory: true
		// 4 = Tool (skip - not a runtime dependency)
		// 5 = Incompatible (skip - breaks compatibility)
		// 6 = Include (skip - bundled with the mod)

		switch dep.RelationType {
		case 2:
			result.Dependencies = append(
				result.Dependencies, catalog.Dependency{
					ID: catalog.VersionedRef{
						Ref: catalog.Ref{
							Name: catalog.Name(
								fmt.Sprintf(
									"%d",
									dep.ModId,
								),
							),
						},
					},
					Mandatory: false,
				},
			)
		case 3:
			result.Dependencies = append(
				result.Dependencies, catalog.Dependency{
					ID: catalog.VersionedRef{
						Ref: catalog.Ref{
							Name: catalog.Name(
								fmt.Sprintf(
									"%d",
									dep.ModId,
								),
							),
						},
					},
					Mandatory: true,
				},
			)
		default:
			continue
		}
	}

	return result
}

// ResolveVersionSelector resolves abstract version specifiers (any, stable,
// beta) to a concrete version by querying the CurseForge API.
func (p *Provider) ResolveVersionSelector(
	ctx context.Context,
	local upstream.LocalContext,
	id catalog.VersionedRef,
) (parsed catalog.VersionedRef, err error) {
	if id.Ecosystem.IsUnspecified() {
		id.Ecosystem = catalog.EcosystemUnspecified
	}
	parsed.Ecosystem = id.Ecosystem
	parsed.Name = id.Name

	mod, err := p.resolveSlug(ctx, id.Name)
	if err != nil {
		return id, err
	}

	var file *fileResponse
	switch id.Version {
	case version.Stable, version.Beta, version.Any:
		file, err = p.latestFile(ctx, mod.ID, local, id.Ecosystem, id.Version)
	default:
		return id, nil
	}
	if err != nil {
		return id, err
	}

	parsed.Version = version.Version(file.FileName)
	return parsed, nil
}
