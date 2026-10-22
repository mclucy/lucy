package resolve

import (
	"context"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	serverpkg "github.com/mclucy/lucy/server"

	versionpkg "github.com/mclucy/lucy/version"
)

type (
	projectKey struct {
		environment serverpkg.Environment
		ecosystem   catalog.Ecosystem
		provider    catalog.Provider
		project     string
	}
	projectRequest struct {
		key          projectKey
		display      string
		requirements []lockfile.RequirementBinding
		releaseIDs   []string
	}
)

// Target identifies the server runtime whose compatibility rules package
// resolution enforces.
type Target struct {
	Product      serverpkg.Product
	Bootstrapper string
	Minecraft    versionpkg.Version
	CoreVersion  versionpkg.Version
	MCDRVersion  string
}

// SupportsPackages reports whether the target product loads packages from
// ecosystem. A paperclip server loads Paper and Bukkit packages regardless of
// how the manifest names its product.
func SupportsPackages(target Target, ecosystem catalog.Ecosystem) bool {
	switch target.Product {
	case serverpkg.ProductFabric:
		return ecosystem == catalog.EcosystemFabric
	case serverpkg.ProductForge:
		return ecosystem == catalog.EcosystemForge
	case serverpkg.ProductNeoForge:
		return ecosystem == catalog.EcosystemNeoForge
	case serverpkg.ProductPaper, serverpkg.ProductPurpur:
		return ecosystem == catalog.EcosystemPaper || ecosystem == catalog.EcosystemBukkit
	default:
		return target.Bootstrapper == "paperclip" &&
			(ecosystem == catalog.EcosystemPaper || ecosystem == catalog.EcosystemBukkit)
	}
}

// PackagesRequest describes one package resolution. Trace and Context
// default to their zero values, which report nothing and never cancel.
type PackagesRequest struct {
	Context            context.Context
	Manifest           *manifest.Document
	Previous           []lockfile.Package
	Target             Target
	Materialize        func(*catalog.Artifact) (string, error)
	WithOptional       bool
	AcceptBroken       bool
	ExpandDependencies bool
	Trace              Trace
}
