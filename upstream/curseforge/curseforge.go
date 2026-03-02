package curseforge

import (
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

type provider struct{}

var Provider provider

func (provider) Source() types.Source {
	return types.SourceCurseForge
}

func (provider) Search(
	query string,
	options types.SearchOptions,
) (res upstream.RawSearchResults, err error) {
	panic("TODO: implement curseforge provider Search")
}

func (provider) Fetch(
	id types.PackageId,
) (remote upstream.RawPackageRemote, err error) {
	panic("TODO: implement curseforge provider Fetch")
}

func (provider) Information(
	name types.ProjectName,
) (info upstream.RawProjectInformation, err error) {
	panic("TODO: implement curseforge provider Information")
}

func (provider) Dependencies(
	id types.PackageId,
) (deps upstream.RawPackageDependencies, err error) {
	panic("TODO: implement curseforge provider Dependencies")
}

func (provider) Support(
	name types.ProjectName,
) (supports upstream.RawProjectSupport, err error) {
	panic("TODO: implement curseforge provider Support")
}

func (provider) ParseAmbiguousVersion(
	id types.PackageId,
) (parsed types.PackageId, err error) {
	panic("TODO: implement curseforge provider ParseAmbiguousVersion")
}
