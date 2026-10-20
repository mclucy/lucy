package types

// DependencySet carries dependency metadata and records whether the
// dependencies were read from the artifact itself.
type DependencySet struct {
	Dependencies []Dependency
	FromArtifact bool
}

// ResolvedPackage is an upstream package coordinate with download metadata.
type ResolvedPackage struct {
	ID            VersionedPackageRef
	FileURL       string
	Filename      string
	Hash          string
	HashAlgorithm string
}

// DiscoveredPackage is a package observed in a server directory.
type DiscoveredPackage struct {
	ID           VersionedPackageRef
	Path         string
	Dependencies DependencySet
}
