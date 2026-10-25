package resolve

import (
	"context"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	serverpkg "github.com/mclucy/lucy/server"

	versionpkg "github.com/mclucy/lucy/version"
)

type (
	requestOrigin uint8
	releaseHint   struct {
		releaseID string
	}
	projectKey struct {
		environment serverpkg.Environment
		ecosystem   catalog.Ecosystem
		provider    catalog.Provider
		project     string
	}
	projectRequest struct {
		key          projectKey
		display      string
		origin       requestOrigin
		requirements []lockfile.RequirementBinding
		dependencyOf catalog.Ref
		// releaseHints come from remote dependency metadata. They order
		// candidates but do not by themselves reject another build.
		releaseHints []releaseHint
	}
)

const (
	originManifest requestOrigin = iota
	originDependency
)

func (o requestOrigin) String() string {
	switch o {
	case originManifest:
		return "manifest"
	case originDependency:
		return "dependency"
	default:
		return "unknown"
	}
}

// Target identifies the server runtime whose compatibility rules package
// resolution enforces.
type Target struct {
	Product      serverpkg.Product
	Bootstrapper string
	Minecraft    versionpkg.Version
	CoreVersion  versionpkg.Version
	MCDRVersion  string
	// CoreModules are the descriptor modules the server core artifact ships.
	// Fabric's loader bundles mods as nested jars, so these satisfy package
	// requirements that no package provides.
	CoreModules []catalog.DescriptorModule
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
	Context  context.Context
	Manifest *manifest.Document
	Previous []lockfile.Package
	Target   Target
	// Materialize must leave its input unchanged and return verified local bytes.
	Materialize        func(*catalog.Artifact) (artifact.MaterializedFile, error)
	WithOptional       bool
	AcceptBroken       bool
	ExpandDependencies bool
	// Cache reuses artifact analysis across candidate combinations. A nil
	// cache analyzes every candidate from scratch.
	Cache *AnalysisCache
	Trace Trace
}
