package spiget

import (
	"errors"
	"fmt"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

type provider struct{}

var Provider provider

func (provider) Provider() types.Provider {
	return types.ProviderSpiget
}

func (p provider) Search(q upstream.Query) (upstream.SearchResponse, error) {
	// Spiget publishes server resources only; IncludeClient needs no filter.
	if q.FilterEcosystem == types.EcosystemBukkit || q.FilterEcosystem == types.EcosystemPaper {
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
	id types.VersionedPackageRef,
) (types.ResolvedPackage, error) {
	resource, err := resolveResourceByProjectName(id.Name)
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	resolved, err := resolveVersion(resource, id.Version)
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	r := resolved.ToPackageRemote()
	return types.ResolvedPackage{
		FileURL:       r.FileURL,
		Filename:      r.Filename,
		Hash:          r.Hash,
		HashAlgorithm: r.HashAlgorithm,
	}, nil
}

func (p provider) Info(ref types.PackageRef) (types.Metadata, error) {
	resource, err := resolveResourceByProjectName(ref.Name)
	if err != nil {
		return types.Metadata{}, err
	}
	info := resource.ToProjectInformation()
	return info, nil
}

func (p provider) Dependencies(
	_ upstream.LocalContext,
	_ types.VersionedPackageRef,
) (*types.DependencySet, error) {
	return &types.DependencySet{FromArtifact: false}, nil
}

func (p provider) ResolveVersionSelector(
	_ upstream.LocalContext,
	id types.VersionedPackageRef,
) (parsed types.VersionedPackageRef, err error) {
	parsed = id
	switch id.Version {
	case "", types.VersionAny, types.VersionBeta, types.VersionStable:
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
