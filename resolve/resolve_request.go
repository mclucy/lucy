package resolve

import (
	"context"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	serverpkg "github.com/mclucy/lucy/server"
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

// SupportsPackages reports whether the server product loads packages from
// ecosystem. A paperclip server loads Paper and Bukkit packages regardless of
// how the manifest names its product.
func SupportsPackages(server lockfile.Server, ecosystem catalog.Ecosystem) bool {
	switch server.Product {
	case serverpkg.ProductFabric:
		return ecosystem == catalog.EcosystemFabric
	case serverpkg.ProductForge:
		return ecosystem == catalog.EcosystemForge
	case serverpkg.ProductNeoForge:
		return ecosystem == catalog.EcosystemNeoForge
	case serverpkg.ProductPaper, serverpkg.ProductPurpur:
		return ecosystem == catalog.EcosystemPaper || ecosystem == catalog.EcosystemBukkit
	default:
		return server.Bootstrapper == "paperclip" &&
			(ecosystem == catalog.EcosystemPaper || ecosystem == catalog.EcosystemBukkit)
	}
}

// PackagesRequest describes one package resolution. Trace and Context
// default to their zero values, which report nothing and never cancel.
type PackagesRequest struct {
	Context            context.Context
	Manifest           *manifest.Document
	Previous           *lockfile.Document
	Server             lockfile.Server
	MCDRVersion        string
	Materialize        func(*catalog.Artifact) (string, error)
	WithOptional       bool
	AcceptBroken       bool
	ExpandDependencies bool
	Trace              Trace
}
