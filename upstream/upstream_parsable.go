package upstream

import "github.com/mclucy/lucy/types"

type SupportedPlatformsReporter interface {
	SupportedPlatforms() []types.PlatformId
}

type DependencyResolver interface {
	ResolveDependency() []types.Dependency
}

type ArtifactMapper interface {
	NameByHash(artifact Hashable) RemotePackageName
	VersionedRefByHash(artifact Hashable) types.VersionedPackageRef
}

type Hashable interface{}

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

type VersionSelectorResolver interface{}
