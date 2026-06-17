package upstream

import (
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

type DependencyLister interface {
	Dependencies(id types.VersionedPackageRef) (*types.PackageDependencies, error)
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
	DependencyLister
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
	InstallPlatform(id types.VersionedPackageRef, serverDir string) error
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
