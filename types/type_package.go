package types

// Package is a package identifier with its related information. In principle,
// only packages remote and local can provide a Package.
//
// This is an adapter type that uses composition method to provide a unified
// interface for both local and remote packages. It is used to represent a
// package in the system, and can be used to store information about the package
// such as its dependencies, installation path, and remote source.
//
// Deprecated: The goal is to eliminate the need for this type by using more specific types such as LocalPackage and RemotePackage.
type Package struct {
	// Id is the basic package identifier
	Id VersionedPackageRef

	// Package specific data
	Dependencies *PackageDependencies
	Local        *PackageInstallation
	Remote       *ResolvedPackage
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

// PlatformSupport reflects the support information of the whole project. For
// specific dependency of a single package, use the PackageDependencies struct.
type PlatformSupport struct {
	MinecraftVersions []BareVersion
	Platforms         []PlatformId
	Authentic         bool
}

// ResolvedPackage — upstream identity + download info. No local state, no deps.
// Produced by: upstream providers (FetchResult assembly)
// Consumed by: install/ resolve/download stages
type ResolvedPackage struct {
	Id            FullPackageRef
	FileUrl       string
	Filename      string
	Hash          string
	HashAlgorithm string
}

// DiscoveredPackage — found on disk via jar scanning.
// Produced by: workspace/probe
// Consumed by: cmd/init/, cmd/cmd_status.go
type DiscoveredPackage struct {
	Id           VersionedPackageRef
	Path         string
	Dependencies PackageDependencies // authentic, from jar analysis
}

// InstalledPackage — resolved + placed + verified.
// Produced by: install/ apply stage
// Consumed by: cmd/cmd_add.go (lock file writer)
type InstalledPackage struct {
	ResolvedPackage
	Path         string
	Dependencies PackageDependencies // authentic, from jar analysis
}
