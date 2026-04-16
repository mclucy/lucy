package spiget

import (
	"errors"

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
	return nil, ErrNotImplemented
}

func (p provider) Fetch(id types.PackageId) (
	remote upstream.RawPackageRemote,
	err error,
) {
	return nil, ErrNotImplemented
}

func (p provider) Information(name types.ProjectName) (
	info upstream.RawProjectInformation,
	err error,
) {
	return nil, ErrNotImplemented
}

func (p provider) Support(name types.ProjectName) (
	supports upstream.RawProjectSupport,
	err error,
) {
	return nil, ErrNotImplemented
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
	return id, ErrNotImplemented
}

var ErrNotImplemented = errors.New("spiget: not implemented")
