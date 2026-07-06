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

func (provider) Id() types.SourceId {
	return types.SourceSpiget
}

func (p provider) Search(q upstream.Query) (upstream.SearchResponse, error) {
	options := upstream.SearchOptions{
		IncludeClient:   !q.ExcludeClient,
		FilterEcosystem: q.FilterEcosystem,
	}
	if options.FilterEcosystem == types.EcoBukkit {
		log.Debug("spiget: platform filter is not supported upstream; search will run without a platform query parameter")
	}

	resp, err := searchResources(q.Keyword, options)
	if err != nil {
		return upstream.SearchResponse{}, err
	}
	return resp.ToSearchResults(p.Id()), nil
}

func (p provider) Fetch(id types.VersionedPackageRef) (
	types.ResolvedPackage,
	error,
) {
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
		FileUrl:       r.FileUrl,
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
	info.From = p.Id()
	return info, nil
}

func (p provider) Dependencies(
	id types.VersionedPackageRef,
) (*types.PackageDependencies, error) {
	return &types.PackageDependencies{Authentic: false}, nil
}

func (p provider) ResolveVersionSelector(id types.VersionedPackageRef) (
	parsed types.VersionedPackageRef,
	err error,
) {
	parsed = id

	switch id.Version {
	case "", types.VersionAny, types.VersionBeta, types.VersionStable, types.VersionNone:
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
	log.Debug("parsed from " + id.StringFull() + " to " + parsed.StringFull())
	return parsed, nil
}

var (
	ErrNoProject = errors.New("spiget: project not found")
	ErrNoVersion = errors.New("spiget: version not found")
)

func unexpectedStatusError(url string, statusCode int) error {
	return fmt.Errorf("spiget: unexpected status %d for %s", statusCode, url)
}
