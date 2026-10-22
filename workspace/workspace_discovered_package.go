package workspace

import "github.com/mclucy/lucy/catalog"

// DiscoveredPackage associates a local package file with its identity.
// Dependencies come from the local descriptor, when present.
type DiscoveredPackage struct {
	ID           catalog.VersionedRef
	Path         string
	Dependencies []catalog.Dependency
}
