package spiget

import (
	"context"
	"errors"
	"fmt"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
)

// Provider resolves Spiget resources and versions through one configured
// network cache. logger may be nil, which records nothing.
type Provider struct {
	network *cache.Client
	logger  *log.Logger
}

// New constructs a Spiget provider. network supplies the cached HTTP
// transport; construction performs no requests.
func New(network *cache.Client, logger *log.Logger) *Provider {
	return &Provider{network: network, logger: logger}
}

func (p *Provider) Provider() catalog.Provider {
	return catalog.ProviderSpiget
}

// Builds lists every published resource version. Spiget exposes no per-version
// game versions, loaders, or channel, so those stay unknown.
func (p *Provider) Builds(ctx context.Context, project string) ([]upstream.Build, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resource, err := p.resolveResourceByProjectName(ctx, catalog.Name(project))
	if err != nil {
		return nil, err
	}
	versions, err := p.listVersions(ctx, resource.ID)
	if err != nil {
		return nil, err
	}

	builds := make([]upstream.Build, 0, len(versions))
	for _, v := range versions {
		builds = append(builds, v.ToBuild())
	}
	return builds, nil
}

func (p *Provider) Search(ctx context.Context, q upstream.Query) (upstream.SearchResponse, error) {
	// Spiget publishes server resources only; IncludeClient needs no filter.
	if q.FilterEcosystem == catalog.EcosystemBukkit || q.FilterEcosystem == catalog.EcosystemPaper {
		p.logger.Debug("spiget: platform filter is not supported upstream; search will run without a platform query parameter")
	}

	resp, err := p.searchResources(ctx, q.Keyword)
	if err != nil {
		return upstream.SearchResponse{}, err
	}
	return resp.ToSearchResults(p.Provider()), nil
}

func (p *Provider) Fetch(
	ctx context.Context,
	_ upstream.LocalContext,
	id catalog.VersionedRef,
) (upstream.ResolvedPackage, error) {
	resource, err := p.resolveResourceByProjectName(ctx, id.Name)
	if err != nil {
		return upstream.ResolvedPackage{}, err
	}

	resolved, err := p.resolveVersion(ctx, resource, id.Version)
	if err != nil {
		return upstream.ResolvedPackage{}, err
	}

	r := resolved.ToPackageRemote()
	return upstream.ResolvedPackage{
		FileURL:       r.FileURL,
		Filename:      r.Filename,
		Hash:          r.Hash,
		HashAlgorithm: r.HashAlgorithm,
	}, nil
}

func (p *Provider) Info(ctx context.Context, ref catalog.Ref) (catalog.Metadata, error) {
	resource, err := p.resolveResourceByProjectName(ctx, ref.Name)
	if err != nil {
		return catalog.Metadata{}, err
	}
	info := resource.ToProjectInformation()
	return info, nil
}

func (p *Provider) Dependencies(
	_ context.Context,
	_ upstream.LocalContext,
	_ catalog.VersionedRef,
) (*upstream.DependencySet, error) {
	return &upstream.DependencySet{FromArtifact: false}, nil
}

func (p *Provider) ResolveVersionSelector(
	ctx context.Context,
	_ upstream.LocalContext,
	id catalog.VersionedRef,
) (parsed catalog.VersionedRef, err error) {
	parsed = id
	switch id.Version {
	case "", version.Any, version.Beta, version.Stable:
	default:
		return id, nil
	}

	resource, err := p.resolveResourceByProjectName(ctx, id.Name)
	if err != nil {
		return id, err
	}

	resolved, err := p.resolveVersion(ctx, resource, id.Version)
	if err != nil {
		return id, err
	}

	parsed.Version = resolved.LucyVersion()
	p.logger.Debug("parsed from " + id.StringQualified() + " to " + parsed.StringQualified())
	return parsed, nil
}

var (
	ErrNoProject = errors.New("spiget: project not found")
	ErrNoVersion = errors.New("spiget: version not found")
)

func unexpectedStatusError(url string, statusCode int) error {
	return fmt.Errorf("spiget: unexpected status %d for %s", statusCode, url)
}
