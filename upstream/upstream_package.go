package upstream

import "github.com/mclucy/lucy/catalog"

// DependencySet carries dependency metadata and records whether the
// dependencies were read from the artifact itself.
type DependencySet struct {
	Dependencies []catalog.Dependency
	FromArtifact bool
}

// ResolvedPackage is an upstream package coordinate with download metadata.
type ResolvedPackage struct {
	ID            catalog.VersionedRef
	FileURL       string
	Filename      string
	Hash          string
	HashAlgorithm string
}
