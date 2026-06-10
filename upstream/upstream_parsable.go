package upstream

import (
	"crypto/sha1"

	"github.com/mclucy/lucy/types"
)

type SupportedPlatformsReporter interface {
	SupportedPlatforms() []types.PlatformId
}

type DependencyResolver interface {
	ResolveDependency() []types.Dependency
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
