package remote

import "github.com/mclucy/lucy/types"

type SourceHandler interface {
	Search(query string, options types.SearchOptions) (
		res RawSearchResults,
		err error,
	)
	Fetch(id types.PackageId) (
		remote RawPackageRemote,
		err error,
	)
	Information(name types.ProjectName) (
		info RawProjectInformation,
		err error,
	)
	Dependencies(id types.PackageId) (
		deps RawPackageDependencies,
		err error,
	)
	Support(name types.ProjectName) (
		supports RawProjectSupport,
		err error,
	)
	ParseAmbiguousVersion(id types.PackageId) (
		parsed types.PackageId,
		err error,
	)
	Name() types.Source
}

// Raw interfaces are designed for lazy evaluation and conversion to typed
// structures only when necessary. More functionality can be added to these
// interfaces as needed.

type (
	RawProjectSupport interface {
		ToProjectSupport() types.PlatformSupport
	}
	RawProjectInformation interface {
		ToProjectInformation() types.ProjectInformation
	}
	RawPackageRemote interface {
		ToPackageRemote() types.PackageRemote
	}
	RawPackageDependencies interface {
		ToPackageDependencies() types.PackageDependencies
	}

	// TODO: Consider make SortBy a method on the RawSearchResults interface

	RawSearchResults interface {
		ToSearchResults() types.SearchResults
	}
)
