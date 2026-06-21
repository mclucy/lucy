package upstream

import (
	"crypto/sha1"
	"strings"

	"github.com/mclucy/lucy/types"
)

// SourceIdentifier returns the semantic source identity represented by a
// provider capability.
type SourceIdentifier interface {
	Id() types.SourceId
}

type Fetcher interface {
	Fetch(id types.VersionedPackageRef) (FetchResult, error)
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

type PlatformInstaller interface {
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
}

type ArtifactResolver interface {
	ResolveArtifact() ResolvedArtifact
}

type ResolvedArtifact struct {
	Ref           types.PackageRef
	Version       types.BareVersion
	Source        types.SourceId
	FileURL       string
	Filename      string
	Hash          string
	HashAlgorithm string
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
	SortBy         types.SearchSort
	ExcludeClient  bool
	FilterPlatform types.PlatformId
	Tags           []string
	Limit          int
}

type SearchResponse struct {
	// Source labels which upstream catalog produced this result set.
	// It is a semantic provenance marker, not a provider instance.
	Source   types.SourceId
	Items    []RemotePackageName
	Warnings []error
}

type Informer interface {
	Info(ref types.PackageRef) (info types.Metadata, err error)
}

type FetchResult struct {
	ResolvedID types.VersionedPackageRef
	Source     types.SourceId
	FileURL    string
	Filename   string
	Hash       string

	// HashAlgorithm names the upstream-provided digest algorithm, such as
	// "sha1" or "sha512". Empty means Hash is unavailable.
	HashAlgorithm string
}

func NewFetchResult(remote types.PackageRemote) FetchResult {
	return FetchResult{
		Source:        remote.Source,
		FileURL:       remote.FileUrl,
		Filename:      remote.Filename,
		Hash:          remote.Hash,
		HashAlgorithm: remote.HashAlgorithm,
	}
}

func (r FetchResult) PackageRemote() types.PackageRemote {
	return types.PackageRemote{
		Source:        r.Source,
		FileUrl:       r.FileURL,
		Filename:      r.Filename,
		Hash:          r.Hash,
		HashAlgorithm: r.HashAlgorithm,
	}
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
