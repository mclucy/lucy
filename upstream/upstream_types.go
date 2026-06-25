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

type Fetcher interface {
	Fetch(id types.VersionedPackageRef) (types.ResolvedPackage, error)
}

type DependencyResolver interface {
	Dependencies(id types.VersionedPackageRef) (
		*types.PackageDependencies,
		error,
	)
}

type SupportReporter interface {
	Support(name types.BarePackageName) (types.PlatformSupport, error)
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

type PlatformProvider interface {
	SourceIdentifier
	VersionSelectorResolver
	Fetcher
}

type SupportedPlatformsReporter interface {
	SupportedPlatforms() []types.PlatformId
}

type ArtifactMapper interface {
	NameByHash(artifact Hashable) (
		name RemotePackageName,
		hash string,
		err error,
	)
}

type Hashable interface {
	Sha1() [sha1.Size]byte
	MurmurHash() uint32
}

type VersionSelectorResolver interface {
	ResolveVersionSelector(ref types.VersionedPackageRef) (
		resolved types.VersionedPackageRef,
		err error,
	)
}

type Searcher interface {
	Search(q Query) (resp SearchResponse, err error)
}

type Query struct {
	Keyword        string
	ExcludeClient  bool
	FilterPlatform types.PlatformId
	Tags           []string
	Limit          int
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

type RemotePackageName struct {
	RemoteName string
	Source     types.SourceId
}

func (r RemotePackageName) FormattedName() string {
	if r.Source == types.SourceMCDR {
		return strings.ReplaceAll(r.RemoteName, "_", "-")
	}
	return r.RemoteName
}

type SearchOptions struct {
	IncludeClient  bool
	FilterPlatform types.PlatformId
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
