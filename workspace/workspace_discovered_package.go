package workspace

import (
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
)

// DiscoveredPackage is a package observed in a server directory.
type DiscoveredPackage struct {
	ID           catalog.VersionedRef
	Path         string
	Dependencies upstream.DependencySet
}
