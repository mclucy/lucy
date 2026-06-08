package types

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
	Information *Metadata
}

// PackageDependencies is one of the optional attributions that can be added to
// a Package struct. It is usually used in any command that requires operating
// local packages, such as `lucy install` or `lucy remove`.
type PackageDependencies struct {
	Value     []Dependency
	Authentic bool
}

// PackageInstallation is an optional attribution to types.Package. It is
// used for packages that are known to be installed in the local filesystem.
type PackageInstallation struct {
	Path string
}

// PackageRemote is an optional attribution to types.Package. It is used to
// represent package's presence in a remote source.
type PackageRemote struct {
	// Source is the semantic origin label of this package metadata/artifact.
	// It is stored and displayed as provenance, not used as an executable
	// provider identifier.
	Source        Source
	FileUrl       string
	Filename      string
	Hash          string // upstream-provided digest; empty if unavailable
	HashAlgorithm string // e.g. "sha1", "sha512"; empty if Hash is empty
}

// PlatformSupport reflects the support information of the whole project. For
// specific dependency of a single package, use the PackageDependencies struct.
type PlatformSupport struct {
	MinecraftVersions []BareVersion
	Platforms         []Platform
	Authentic         bool
}
