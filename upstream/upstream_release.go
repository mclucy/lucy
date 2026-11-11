package upstream

import (
	"context"
	"slices"

	"github.com/mclucy/lucy/catalog"
)

// ArtifactReleaseMapper resolves an installed artifact to the release it was
// published as, so a caller adopts the file already on disk without listing
// the project's releases.
type ArtifactReleaseMapper interface {
	ProviderIdentifier
	ReleaseByHash(
		ctx context.Context,
		artifact ArtifactFingerprint,
	) (Release, bool, error)
}

// Release is one published artifact as its provider records it. Ref names the
// project and version; Candidate describes the same file for resolution.
// Compatibility labels are advisory: installed bytes prove the server holds the
// artifact, so a release whose labels miss the target stays installable.
type Release struct {
	Ref               catalog.VersionedRef
	Candidate         Candidate
	MinecraftVersions []string
	Loaders           []string
}

// Supports reports whether the release labels itself for minecraft and loader.
// An empty minecraft or unspecified loader asks nothing. Labels that name no
// ecosystem this build models answer nothing either.
func (r Release) Supports(minecraft string, loader catalog.Ecosystem) bool {
	if minecraft != "" && len(r.MinecraftVersions) > 0 &&
		!slices.Contains(r.MinecraftVersions, minecraft) {
		return false
	}
	return r.SupportsLoader(loader)
}

// SupportsLoader reports whether the release names a loader that loader runs.
// An unspecified loader asks nothing. Labels that name no modeled ecosystem
// answer nothing either.
func (r Release) SupportsLoader(loader catalog.Ecosystem) bool {
	if loader == catalog.EcosystemUnspecified {
		return true
	}
	recognized := false
	for _, label := range r.Loaders {
		ecosystem := catalog.Ecosystem(label)
		if !knownEcosystem(ecosystem) {
			continue
		}
		recognized = true
		if loader.Satisfy(ecosystem) {
			return true
		}
	}
	return !recognized
}

func knownEcosystem(ecosystem catalog.Ecosystem) bool {
	switch ecosystem {
	case catalog.EcosystemFabric,
		catalog.EcosystemQuilt,
		catalog.EcosystemForge,
		catalog.EcosystemNeoForge,
		catalog.EcosystemBukkit,
		catalog.EcosystemPaper,
		catalog.EcosystemSponge,
		catalog.EcosystemBungeeCord,
		catalog.EcosystemVelocity,
		catalog.EcosystemMCDR:
		return true
	default:
		return false
	}
}
