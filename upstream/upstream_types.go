package upstream

import (
	"crypto/sha1"
	"strings"
	"time"

	"github.com/mclucy/lucy/types"
)

// ProviderIdentifier exposes the execution-layer identity of a provider
// capability.
type ProviderIdentifier interface {
	Provider() types.Provider
}

// LocalContext is an immutable snapshot of local runtime facts that can affect
// upstream resolution. Its zero value describes an unknown local runtime.
//
// The application layer derives LocalContext from workspace discovery. Upstream
// must not depend on that discovery mechanism or its filesystem representation.
type LocalContext struct {
	Ecosystem        types.Ecosystem
	MinecraftVersion types.Version
	MCDRVersion      types.Version
}

// HasMinecraftVersion reports whether the local runtime provides a concrete
// Minecraft version suitable for upstream compatibility filtering.
func (c LocalContext) HasMinecraftVersion() bool {
	return c.MinecraftVersion != "" &&
		!c.MinecraftVersion.IsInvalid() &&
		!c.MinecraftVersion.CanInfer()
}

type Fetcher interface {
	Fetch(local LocalContext, id types.VersionedPackageRef) (types.ResolvedPackage, error)
}

type DependencyResolver interface {
	Dependencies(local LocalContext, id types.VersionedPackageRef) (
		*types.DependencySet,
		error,
	)
}

type PackageResolver interface {
	ProviderIdentifier
	VersionSelectorResolver
	Fetcher
}

type PackageProvider interface {
	PackageResolver
	DependencyResolver
}

type SearchProvider interface {
	ProviderIdentifier
	Searcher
}

type InfoProvider interface {
	ProviderIdentifier
	Informer
}

type ArtifactMapProvider interface {
	ProviderIdentifier
	ArtifactMapper
}

// InstallerProvider installs a server product for one ecosystem.
type InstallerProvider interface {
	ProviderIdentifier
	VersionSelectorResolver
	Fetcher
}

type ArtifactMapper interface {
	PackageByHash(artifact ArtifactFingerprint) (
		ref types.VersionedPackageRef,
		hash string,
		ok bool,
		err error,
	)
}

type ArtifactFingerprint interface {
	SHA1() ([sha1.Size]byte, error)
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
	IncludeClient   bool
	FilterEcosystem types.Ecosystem
	Tags            []string
	Limit           int
}

type SearchResponse struct {
	// Provider labels which upstream provider produced this result set.
	Provider types.Provider `json:"provider"`
	Items    []SearchResult `json:"items"`
	Warnings []error        `json:"warnings,omitempty"`
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

// SearchResult represents a single search result with optional metadata.
// Providers populate what their API returns; display layer handles zero values.
type SearchResult struct {
	ProjectName string         `json:"project"`
	Provider    types.Provider `json:"provider"`

	// Optional metadata — zero value means unavailable.
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	Downloads   int64     `json:"downloads,omitempty"`
	LastUpdated time.Time `json:"last_updated,omitzero"`
}

func (r SearchResult) DisplayName() string {
	if r.Provider == types.ProviderMCDR {
		return strings.ReplaceAll(r.ProjectName, "_", "-")
	}
	return r.ProjectName
}
