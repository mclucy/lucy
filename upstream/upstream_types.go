package upstream

import (
	"crypto/sha1"
	"strings"
	"time"

	"github.com/mclucy/lucy/types"
)

// SourceIdentifier returns the semantic source identity represented by a
// provider capability.
type SourceIdentifier interface {
	Id() types.SourceId
}

// LocalContext is an immutable snapshot of local runtime facts that can affect
// upstream resolution. Its zero value describes an unknown local runtime.
//
// The application layer derives LocalContext from workspace discovery. Upstream
// must not depend on that discovery mechanism or its filesystem representation.
type LocalContext struct {
	ModLoader   types.Ecosystem
	GameVersion types.BareVersion
	MCDRVersion types.BareVersion
}

// HasGameVersion reports whether the local runtime provides a concrete
// Minecraft version suitable for upstream compatibility filtering.
func (c LocalContext) HasGameVersion() bool {
	return c.GameVersion != "" &&
		!c.GameVersion.IsInvalid() &&
		!c.GameVersion.CanInfer()
}

type Fetcher interface {
	Fetch(local LocalContext, id types.VersionedPackageRef) (types.ResolvedPackage, error)
}

type DependencyResolver interface {
	Dependencies(local LocalContext, id types.VersionedPackageRef) (
		*types.PackageDependencies,
		error,
	)
}

type PackageResolver interface {
	SourceIdentifier
	VersionSelectorResolver
	Fetcher
}

type PackageSource interface {
	PackageResolver
	DependencyResolver
}

type SearchSource interface {
	SourceIdentifier
	Searcher
}

type InfoSource interface {
	SourceIdentifier
	Informer
}

type ArtifactMapSource interface {
	SourceIdentifier
	ArtifactMapper
}

type EcosystemProvider interface {
	SourceIdentifier
	VersionSelectorResolver
	Fetcher
}

type ArtifactMapper interface {
	PackageByHash(artifact Hashable) (
		ref types.VersionedPackageRef,
		hash string,
		ok bool,
		err error,
	)
}

type Hashable interface {
	Sha1() ([sha1.Size]byte, error)
	MurmurHash() (uint32, error)
}

type VersionSelectorResolver interface {
	ResolveVersionSelector(local LocalContext, ref types.VersionedPackageRef) (
		resolved types.VersionedPackageRef,
		err error,
	)
}

type Searcher interface {
	Search(q Query) (resp SearchResponse, err error)
}

type Query struct {
	Keyword         string
	ExcludeClient   bool
	FilterEcosystem types.Ecosystem
	Tags            []string
	Limit           int
}

type SearchResponse struct {
	// Source labels which upstream catalog produced this result set.
	// It is a semantic provenance marker, not a provider instance.
	Source   types.SourceId
	Items    []SearchResult
	Warnings []error
}

type Informer interface {
	Info(ref types.PackageRef) (info types.Metadata, err error)
}

// Info is a project description from one upstream. Ref identifies the
// upstream project that answered. Metadata is the content it returned.
type Info struct {
	Ref      types.PackageRef
	Metadata types.Metadata
}

type SearchOptions struct {
	IncludeClient   bool
	FilterEcosystem types.Ecosystem
}

// SearchResult represents a single search result with optional metadata.
// Providers populate what their API returns; display layer handles zero values.
type SearchResult struct {
	RemoteName string
	Source     types.SourceId

	// Optional metadata — zero value means unavailable.
	Title       string
	Description string
	Downloads   int64
	LastUpdated time.Time
}

func (r SearchResult) FormattedName() string {
	if r.Source == types.SourceMCDR {
		return strings.ReplaceAll(r.RemoteName, "_", "-")
	}
	return r.RemoteName
}
