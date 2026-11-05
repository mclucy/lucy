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
	"path"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
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
	switch q.FilterEcosystem {
	case catalog.EcosystemForge:
		facets = append(facets, facetForgeOnly)
	case catalog.EcosystemFabric:
		facets = append(facets, facetFabricOnly)
	case catalog.EcosystemNeoForge:
		facets = append(facets, facetNeoforgeOnly)
	case catalog.EcosystemBukkit:
		facets = append(facets, facetBukkitOnly)
	case catalog.EcosystemUnspecified:
		fallthrough
	default:
		facets = append(facets, facetAllLoaders)
	}

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

func (p *Provider) Fetch(
	ctx context.Context,
	local upstream.LocalContext,
	id catalog.VersionedRef,
) (upstream.ResolvedPackage, error) {
	version, err := p.getVersion(ctx, local, id)
	if err != nil {
		return upstream.ResolvedPackage{}, err
	}
	if len(version.Files) == 0 || path.Ext(version.Files[0].Filename) != ".jar" {
		return upstream.ResolvedPackage{}, ErrUnsupportedFileType
	}
	resolved := resolvedPackageFromVersion(*version)
	resolved.ID.Ref = id.Ref
	resolved.ID.Version = id.Version
	return resolved, nil
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

// Modrinth may publish non-JAR files; Lucy accepts only JAR artifacts.
var ErrUnsupportedFileType = errors.New("modrinth: only .jar files are supported")

func (p *Provider) Dependencies(
	ctx context.Context,
	local upstream.LocalContext,
	id catalog.VersionedRef,
) (*upstream.DependencySet, error) {
	version, err := p.getVersion(ctx, local, id)
	if err != nil {
		return nil, fmt.Errorf("modrinth: dependencies fetch failed: %w", err)
	}
	dependencies, err := (&modrinthDependencies{
		provider: p,
		version:  version,
		platform: id.Ecosystem,
	}).ToDependencySet(ctx)
	if err != nil {
		return nil, err
	}
	return &dependencies, nil
}

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

	var v *versionResponse
	switch id.Version {
	case versionpkg.Stable, versionpkg.Beta, versionpkg.Any:
		v, err = p.latestVersion(
			ctx,
			id.Name,
			id.Ecosystem,
			local.MinecraftVersion,
			id.Version,
		)
	default:
		return id, nil
	}
	if err != nil {
		return id, err
	}
	parsed.Version = versionpkg.Version(v.VersionNumber)
	return parsed, nil
}
