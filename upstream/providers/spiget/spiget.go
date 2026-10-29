package spiget

import (
	"context"
	"errors"
	"fmt"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
)

type provider struct{}

var Provider provider

func (provider) Provider() catalog.Provider {
	return catalog.ProviderSpiget
}

// Builds lists every published resource version. Spiget exposes no per-version
// game versions, loaders, or channel, so those stay unknown.
func (p provider) Builds(ctx context.Context, project string) ([]upstream.Build, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resource, err := resolveResourceByProjectName(catalog.Name(project))
	if err != nil {
		return nil, err
	}
	versions, err := listVersions(resource.ID)
	if err != nil {
		return nil, err
	}

	builds := make([]upstream.Build, 0, len(versions))
	for _, v := range versions {
		builds = append(builds, v.ToBuild())
	}
	return builds, nil
}

func (p provider) Search(q upstream.Query) (upstream.SearchResponse, error) {
	// Spiget publishes server resources only; IncludeClient needs no filter.
	if q.FilterEcosystem == catalog.EcosystemBukkit || q.FilterEcosystem == catalog.EcosystemPaper {
		log.Debug("spiget: platform filter is not supported upstream; search will run without a platform query parameter")
	}

	resp, err := searchResources(q.Keyword)
	if err != nil {
		return upstream.SearchResponse{}, err
	}
	return resp.ToSearchResults(p.Provider()), nil
}

func (p provider) Fetch(
	_ upstream.LocalContext,
	id catalog.VersionedRef,
) (upstream.ResolvedPackage, error) {
	resource, err := resolveResourceByProjectName(id.Name)
	if err != nil {
		return upstream.ResolvedPackage{}, err
	}

	resolved, err := resolveVersion(resource, id.Version)
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

func (p provider) Info(ref catalog.Ref) (catalog.Metadata, error) {
	resource, err := resolveResourceByProjectName(ref.Name)
	if err != nil {
		return catalog.Metadata{}, err
	}
	info := resource.ToProjectInformation()
	return info, nil
}

func (p provider) Dependencies(
	_ upstream.LocalContext,
	_ catalog.VersionedRef,
) (*upstream.DependencySet, error) {
	return &upstream.DependencySet{FromArtifact: false}, nil
}

func (p provider) ResolveVersionSelector(
	_ upstream.LocalContext,
	id catalog.VersionedRef,
) (parsed catalog.VersionedRef, err error) {
	parsed = id
	switch id.Version {
	case "", version.Any, version.Beta, version.Stable:
	default:
		return id, nil
	}

	resource, err := resolveResourceByProjectName(id.Name)
	if err != nil {
		return id, err
	}

	resolved, err := resolveVersion(resource, id.Version)
	if err != nil {
		return id, err
	}

	parsed.Version = resolved.LucyVersion()
	log.Debug("parsed from " + id.StringQualified() + " to " + parsed.StringQualified())
	return parsed, nil
}

var (
	ErrNoProject = errors.New("spiget: project not found")
	ErrNoVersion = errors.New("spiget: version not found")
)

func unexpectedStatusError(url string, statusCode int) error {
	return fmt.Errorf("spiget: unexpected status %d for %s", statusCode, url)
}
