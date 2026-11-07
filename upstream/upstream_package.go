package upstream

import "github.com/mclucy/lucy/catalog"

// ResolvedPackage is an upstream package coordinate with download metadata.
type ResolvedPackage struct {
	ID            catalog.VersionedRef
	FileURL       string
	Filename      string
	Hash          string
	HashAlgorithm string
}
