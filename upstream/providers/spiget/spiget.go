package spiget

import (
	"errors"
	"fmt"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

type provider struct{}

var Provider provider

func (provider) Id() types.SourceId {
	return types.SourceSpiget
}

func (p provider) Search(q upstream.Query) (upstream.SearchResponse, error) {
	options := types.SearchOptions{
		IncludeClient:  !q.ExcludeClient,
		SortBy:         q.SortBy,
		FilterPlatform: q.FilterPlatform,
	}
	if options.FilterPlatform == types.PlatformBukkit {
		logger.Debug("spiget: platform filter is not supported upstream; search will run without a platform query parameter")
	}

	resp, err := searchResources(q.Keyword, options)
	if err != nil {
		return upstream.SearchResponse{}, err
	}
	return resp.ToSearchResults(p.Id()), nil
}

func (p provider) Fetch(id types.VersionedPackageRef) (
	remote upstream.RawPackageRemote,
	err error,
) {
	resource, err := resolveResourceByProjectName(id.Name)
	if err != nil {
		return nil, err
	}

	resolved, err := resolveVersion(resource, id.Version)
	if err != nil {
		return nil, err
	}

	return resolved, nil
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

func (p provider) Support(name types.BarePackageName) (
	supports upstream.RawProjectSupport,
	err error,
) {
	resource, err := resolveResourceByProjectName(name)
	if err != nil {
		return nil, err
	}
	return resource, nil
}

func (p provider) Dependencies(id types.VersionedPackageRef) (
	deps upstream.RawPackageDependencies,
	err error,
) {
	return nil, ErrNotImplemented
}

func (p provider) ResolveVersionSelector(id types.VersionedPackageRef) (
	parsed types.VersionedPackageRef,
	err error,
) {
	parsed = id

	switch id.Version {
	case "", types.VersionAny, types.VersionNone, types.VersionLatest, types.VersionCompatible:
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
	logger.Debug("parsed from " + id.StringFull() + " to " + parsed.StringFull())
	return parsed, nil
}

var (
	ErrNotImplemented = errors.New("spiget: not implemented")
	ErrNoProject      = errors.New("spiget: project not found")
	ErrNoVersion      = errors.New("spiget: version not found")
)

func unexpectedStatusError(url string, statusCode int) error {
	return fmt.Errorf("spiget: unexpected status %d for %s", statusCode, url)
}
