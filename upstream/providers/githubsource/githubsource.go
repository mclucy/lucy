package githubsource

import (
	"errors"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

type provider struct{}

var Provider provider

func (provider) Id() types.SourceId {
	return types.SourceGitHub
}

func (provider) Search(upstream.Query) (upstream.SearchResponse, error) {
	return upstream.SearchResponse{}, errors.New("github provider does not support search")
}

func (provider) Fetch(
	id types.VersionedPackageRef,
) (remote upstream.RawPackageRemote, err error) {
	panic("TODO: implement github provider Fetch")
}

func (provider) Info(
	ref types.PackageRef,
) (info types.Metadata, err error) {
	panic("TODO: implement github provider Information")
}

func (provider) Dependencies(
	id types.VersionedPackageRef,
) (deps upstream.RawPackageDependencies, err error) {
	panic("TODO: implement github provider Dependencies")
}

func (provider) Support(
	name types.BarePackageName,
) (supports upstream.RawProjectSupport, err error) {
	panic("TODO: implement github provider Support")
}

func (provider) ResolveVersionSelector(
	id types.VersionedPackageRef,
) (parsed types.VersionedPackageRef, err error) {
	panic("TODO: implement github provider ResolveVersionSelector")
}
