package types

// PackageDependencies carries dependency metadata and records whether it came
// from the artifact itself.
type PackageDependencies struct {
	Value     []Dependency
	Authentic bool
}

// PackageInstallation records a local filesystem path for a package.
// Deprecated: use DiscoveredPackage.Path or InstalledPackage.Path instead.
type PackageInstallation struct {
	Path string
}

// ResolvedPackage is an upstream package coordinate with download metadata.
type ResolvedPackage struct {
	Id            VersionedPackageRef
	FileUrl       string
	Filename      string
	Hash          string
	HashAlgorithm string
}

// DiscoveredPackage is a package observed in a server directory.
type DiscoveredPackage struct {
	Id           VersionedPackageRef
	Path         string
	Dependencies PackageDependencies
}

// InstalledPackage is a resolved package present at Path after installation
// and verification.
type InstalledPackage struct {
	ResolvedPackage
	Path         string
	Dependencies PackageDependencies
}
