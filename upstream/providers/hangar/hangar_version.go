package hangar

import (
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/types"
)

const hangarPreferredPlatform = "PAPER"

type hangarDependencies struct {
	version  *hangarVersion
	platform types.Ecosystem
}

func (h *hangarDependencies) ToDependencySet() types.DependencySet {
	result := types.DependencySet{FromArtifact: true}
	for _, dep := range h.version.DependenciesForPlatform(h.platform) {
		if dep.Name == "" || dep.ExternalURL != nil {
			continue
		}
		result.Dependencies = append(
			result.Dependencies, types.Dependency{
				ID: types.VersionedPackageRef{
					PackageRef: types.PackageRef{
						Name:     input.ToProjectName(dep.Name),
						Provider: types.ProviderUnspecified,
					},
					Ecosystem: types.EcosystemUnspecified,
				},
				Mandatory: dep.Required,
			},
		)
	}
	return result
}

func resolveVersion(id types.VersionedPackageRef) (*hangarVersion, error) {
	versions, err := listVersions(id.Name)
	if err != nil {
		return nil, err
	}

	switch id.Version {
	case types.VersionAny, types.VersionBeta:
		return selectLatestVersion(versions, id.Ecosystem, id.Version)
	case types.VersionStable:
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

// selectLatestVersion selects the newest version for the any/beta selectors.
// any prefers a release channel and falls back to any channel; beta accepts
// any channel. Platform-specific candidates are preferred over versions that
// declare no platform dependency.
func selectLatestVersion(
	versions []hangarVersion,
	platform types.Ecosystem,
	selector types.Version,
) (*hangarVersion, error) {
	platforms := []types.Ecosystem{platform}
	if platform != types.EcosystemUnspecified {
		platforms = append(platforms, types.EcosystemUnspecified)
	}
	for _, candidatePlatform := range platforms {
		if selector == types.VersionAny {
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
	platform types.Ecosystem,
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
	platform types.Ecosystem,
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

func preferredDownloadPlatform(platform types.Ecosystem) types.Ecosystem {
	if platform == types.EcosystemUnspecified {
		return "paper"
	}
	return "paper"
}
