package upstream

import "github.com/mclucy/lucy/types"

// Provider is the inversion boundary between core upstream orchestration and
// concrete upstream integrations.
//
// Rules:
//   - Core code depends on this interface, never on concrete provider packages.
//   - Provider packages implement this interface and perform upstream-specific
//     API/data handling.
//   - Source selection/fallback policy is handled by dedicated resolver logic
//     outside this file.
type Provider interface {
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
	Source() types.Source
}

// Raw interfaces are internal conversion contracts returned by providers before
// being normalized into types.* structures.

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
