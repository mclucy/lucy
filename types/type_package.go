package types

type UrlType uint8

const (
	UrlFile UrlType = iota
	UrlHome
	UrlSource
	UrlWiki
	UrlForum
	UrlIssues
	UrlSponsor
	UrlMisc
)

func (p UrlType) String() string {
	switch p {
	case UrlFile:
		return "File"
	case UrlHome:
		return "Homepage"
	case UrlSource:
		return "Source"
	case UrlWiki:
		return "Wiki"
	case UrlMisc:
		return "URL"
	default:
		return "Unknown"
	}
}

type Url struct {
	Name string
	Type UrlType
	Url  string
}

// Package is a package identifier with its related information. In principle,
// only packages remote and local can provide a Package.
//
// This is an adapter type that uses composition method to provide a unified
// interface for both local and remote packages. It is used to represent a
// package in the system, and can be used to store information about the package
// such as its dependencies, installation path, and remote source.
type Package struct {
	// Id is the basic package identifier
	Id PackageId

	// Package specific data
	Dependencies *PackageDependencies
	Local        *PackageInstallation
	Remote       *PackageRemote

	// Project data
	Supports    *PlatformSupport
	Information *ProjectInformation
}

// PackageDependencies is one of the optional attributions that can be added to
// a Package struct. It is usually used in any command that requires operating
// local packages, such as `lucy install` or `lucy remove`.
type PackageDependencies struct {
	Value     []Dependency
	Authentic bool
}

// ProjectInformation is a struct that contains informational data about the
// package. It is typically used in `lucy info`.
type ProjectInformation struct {
	Title                 string
	Brief                 string
	Description           string
	DescriptionUrl        string
	DescriptionIsMarkdown bool
	Authors               []Person
	Urls                  []Url
	License               string
}

type (
	Person struct {
		Name  string
		Role  string
		Url   string
		Email string
	}

	// PackageInstallation is an optional attribution to types.Package. It is
	// used for packages that are known to be installed in the local filesystem.
	PackageInstallation struct {
		Path string
	}

	// PackageRemote is an optional attribution to types.Package. It is used to
	// represent package's presence in a remote source.
	PackageRemote struct {
		// This string should come from a remote.Source. It is here because structured
		// sources exists only in the remote package.
		Source   Source
		FileUrl  string
		Filename string

		// Not implementing this for now until I found a good library to handle it.
		// Hash       string
		// HashMethod string
	}

	// PlatformSupport reflects the support information of the whole project. For
	// specific dependency of a single package, use the PackageDependencies struct.
	PlatformSupport struct {
		MinecraftVersions []RawVersion
		Platforms         []Platform
		Authentic         bool
	}
)
