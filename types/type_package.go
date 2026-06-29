package types

// PackageDependencies is one of the optional attributions that can be added to
// a Package struct. It is usually used in any command that requires operating
// local packages, such as `lucy install` or `lucy remove`.
type PackageDependencies struct {
	Value     []Dependency
	Authentic bool
}

// PackageInstallation records a local filesystem path for a package.
// Deprecated: use DiscoveredPackage.Path or InstalledPackage.Path instead.
type PackageInstallation struct {
	Path string
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
