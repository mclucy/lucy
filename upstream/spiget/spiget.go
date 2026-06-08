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

func (provider) Source() types.Source {
	return types.SourceSpiget
}

func (provider) Search(
query string,
options types.SearchOptions,
) (res upstream.RawSearchResults, err error) {
	if options.FilterPlatform == types.PlatformBukkit {
		logger.Debug("spiget: platform filter is not supported upstream; search will run without a platform query parameter")
	}

	resp, err := searchResources(query, options)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (p provider) Fetch(id types.PackageId) (
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

func (p provider) Metadata(name types.PackageName) (
info upstream.RawProjectInformation,
err error,
) {
	resource, err := resolveResourceByProjectName(name)
	if err != nil {
		return nil, err
	}
	return resource, nil
}

func (p provider) Support(name types.PackageName) (
supports upstream.RawProjectSupport,
err error,
) {
	resource, err := resolveResourceByProjectName(name)
	if err != nil {
		return nil, err
	}
	return resource, nil
}

func (p provider) Dependencies(id types.PackageId) (
deps upstream.RawPackageDependencies,
err error,
) {
	return nil, ErrNotImplemented
}

func (p provider) ParseAmbiguousId(id types.PackageId) (
parsed types.PackageId,
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
