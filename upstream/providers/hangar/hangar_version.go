package hangar

import (
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

const hangarPreferredPlatform = "PAPER"

type hangarDependencies struct {
	version  *hangarVersion
	platform catalog.Ecosystem
}

func (h *hangarDependencies) ToDependencySet() upstream.DependencySet {
	result := upstream.DependencySet{FromArtifact: true}
	for _, dep := range h.version.DependenciesForPlatform(h.platform) {
		if dep.Name == "" || dep.ExternalURL != nil {
			continue
		}
		result.Dependencies = append(
			result.Dependencies, catalog.Dependency{
				ID: catalog.VersionedRef{
					Ref: catalog.Ref{
						Name:     input.ToProjectName(dep.Name),
						Provider: catalog.ProviderUnspecified,
					},
					Ecosystem: catalog.EcosystemUnspecified,
				},
				Mandatory: dep.Required,
			},
		)
	}
	return result
}

func resolveVersion(id catalog.VersionedRef) (*hangarVersion, error) {
	versions, err := listVersions(id.Name)
	if err != nil {
		return nil, err
	}

	switch id.Version {
	case versionpkg.Any, versionpkg.Beta:
		return selectLatestVersion(versions, id.Ecosystem, id.Version)
	case versionpkg.Stable:
		return selectLatestCompatibleVersion(versions, id.Ecosystem)
	default:
		for i := range versions {
			if versions[i].Name == id.Version.String() {
				return &versions[i], nil
			}
		}
		return nil, ErrNoVersion
	}
}

// selectLatestVersion selects the newest version for any/beta selectors.
// any prefers a release channel and falls back to any channel; beta accepts
// any channel. Platform-specific candidates are preferred over versions that
// declare no platform dependency.
func selectLatestVersion(
	versions []hangarVersion,
	platform catalog.Ecosystem,
	selector versionpkg.Version,
) (*hangarVersion, error) {
	platforms := []catalog.Ecosystem{platform}
	if platform != catalog.EcosystemUnspecified {
		platforms = append(platforms, catalog.EcosystemUnspecified)
	}
	for _, candidatePlatform := range platforms {
		if selector == versionpkg.Any {
			if version := firstVersionMatching(
				versions,
				candidatePlatform,
				false,
				true,
			); version != nil {
				return version, nil
			}
		}
		if version := firstVersionMatching(
			versions,
			candidatePlatform,
			false,
			false,
		); version != nil {
			return version, nil
		}
	}
	return nil, ErrNoVersion
}

// selectLatestCompatibleVersion selects the newest release-channel version
// that declares compatibility with the platform. stable never falls back to
// a prerelease channel.
func selectLatestCompatibleVersion(
	versions []hangarVersion,
	platform catalog.Ecosystem,
) (*hangarVersion, error) {
	if version := firstVersionMatching(
		versions,
		platform,
		true,
		true,
	); version != nil {
		return version, nil
	}
	return nil, ErrNoVersion
}

func firstVersionMatching(
	versions []hangarVersion,
	platform catalog.Ecosystem,
	requireCompatibility bool,
	requireRelease bool,
) *hangarVersion {
	for i := range versions {
		version := &versions[i]
		if !version.HasDownloadForPlatform(platform) {
			continue
		}
		if requireCompatibility && !version.SupportsPlatform(platform) {
			continue
		}
		if requireRelease && !version.IsReleaseChannel() {
			continue
		}
		return version
	}
	return nil
}

func preferredDownloadPlatform(platform catalog.Ecosystem) catalog.Ecosystem {
	if platform == catalog.EcosystemUnspecified {
		return "paper"
	}
	return "paper"
}
