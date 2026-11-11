package resolve

import (
	"context"
	"slices"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/log"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"

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
		requirements []document.RequirementBinding
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
	Bootstrapper serverpkg.BootstrapMethod
	Minecraft    versionpkg.Version
	CoreVersion  versionpkg.Version
	MCDRVersion  string
	// CoreModules are the descriptor modules the server core artifact ships.
	// Fabric's loader bundles mods as nested jars, so these satisfy package
	// requirements that no package provides.
	CoreModules []catalog.DescriptorModule
}

// PackageRuntime returns the ecosystem whose loader loads server packages on
// target. A paperclip server runs Paper regardless of how the manifest names
// its product. The second result is false for a target that loads no server
// packages through Lucy.
func (t Target) PackageRuntime() (catalog.Ecosystem, bool) {
	switch t.Product {
	case serverpkg.ProductFabric,
		serverpkg.ProductQuilt,
		serverpkg.ProductForge,
		serverpkg.ProductNeoForge,
		serverpkg.ProductPaper,
		serverpkg.ProductPurpur:
		return t.Product.Ecosystem(), true
	}
	if t.Bootstrapper == serverpkg.BootstrapMethodPaperclip {
		return catalog.EcosystemPaper, true
	}
	return catalog.EcosystemUnspecified, false
}

// SupportsPackages reports whether the target loads server packages from
// ecosystem.
func SupportsPackages(target Target, ecosystem catalog.Ecosystem) bool {
	runtime, ok := target.PackageRuntime()
	return ok && slices.Contains(runtime.Loads(), ecosystem)
}

// moduleNamespace returns the ecosystem that scopes module identities of
// descriptor ecosystem d on target.
func (t Target) moduleNamespace(d catalog.Ecosystem) catalog.Ecosystem {
	runtime, _ := t.PackageRuntime()
	return runtime.ModuleNamespace(d)
}

// descriptorLoader returns the loader whose descriptor precedence reads an
// artifact of a package in ecosystem. A runtime that loads the ecosystem
// reads the artifact as its own loader would.
func (t Target) descriptorLoader(ecosystem catalog.Ecosystem) catalog.Ecosystem {
	if runtime, ok := t.PackageRuntime(); ok &&
		slices.Contains(runtime.Loads(), ecosystem) {
		return runtime
	}
	return ecosystem
}

// CandidateSource lists the published builds of one project. Callers inject
// the routing capability that serves their configured providers; resolution
// never selects a provider on its own.
type CandidateSource func(
	ctx context.Context,
	registry catalog.Registry,
	project, minecraft string,
	ecosystem catalog.Ecosystem,
) ([]upstream.Candidate, error)

// PackagesRequest describes one package resolution. Trace, Logger, and Context
// default to their zero values, which report nothing and never cancel. A
// resolution that needs candidates fails when Candidates is nil.
type PackagesRequest struct {
	Context  context.Context
	Manifest *document.Manifest
	Previous []document.Package
	Target   Target
	// Candidates lists the builds a direct or dependency requirement may
	// select. It is required for every non-empty resolution.
	Candidates CandidateSource
	// Materialize must leave its input unchanged and return verified local bytes.
	Materialize  func(*catalog.Artifact) (artifact.MaterializedFile, error)
	WithOptional bool
	AcceptBroken bool
	// AcceptPackageConflicts accepts a graph whose package metadata conflicts
	// or whose package modules cannot satisfy each other, reporting them as
	// warnings after the final selection. Requirements the server runtime
	// itself answers stay fatal. It is separate from AcceptBroken, which
	// relaxes every module check.
	AcceptPackageConflicts bool
	ExpandDependencies     bool
	// Cache reuses artifact analysis across candidate combinations. A nil
	// cache analyzes every candidate from scratch.
	Cache *AnalysisCache
	// Logger records resolution diagnostics. A nil Logger records nothing.
	Logger *log.Logger
	Trace  Trace
}
