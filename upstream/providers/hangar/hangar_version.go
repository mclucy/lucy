package hangar

import (
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

const hangarPreferredPlatform = "PAPER"

type hangarDependencies struct {
	version  *hangarVersion
	platform types.PlatformId
}

var _ upstream.RawPackageDependencies = (*hangarDependencies)(nil)

func (h *hangarDependencies) ToPackageDependencies() types.PackageDependencies {
	result := types.PackageDependencies{Authentic: true}
	for _, dep := range h.version.DependenciesForPlatform(h.platform) {
		if dep.Name == "" || dep.ExternalURL != nil {
			continue
		}
		result.Value = append(
			result.Value, types.Dependency{
				Id: types.VersionedPackageRef{
					Platform: types.PlatformNone,
					Name:     syntax.ToProjectName(dep.Name),
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
	case types.VersionAny, types.VersionNone, types.VersionLatest:
		return selectLatestVersion(versions, id.Platform)
	case types.VersionCompatible:
		return selectLatestCompatibleVersion(versions, id.Platform)
	default:
		for i := range versions {
			if versions[i].Name == id.Version.String() {
				return &versions[i], nil
			}
		}
		return nil, ErrNoVersion
	}
}

func selectLatestVersion(
	versions []hangarVersion,
	platform types.PlatformId,
) (*hangarVersion, error) {
	if version := firstVersionMatching(
		versions,
		platform,
		false,
	); version != nil {
		return version, nil
	}
	if version := firstVersionMatching(
		versions,
		types.PlatformNone,
		false,
	); version != nil {
		return version, nil
	}
	return nil, ErrNoVersion
}

func selectLatestCompatibleVersion(
	versions []hangarVersion,
	platform types.PlatformId,
) (*hangarVersion, error) {
	if version := firstVersionMatching(
		versions,
		platform,
		true,
	); version != nil {
		return version, nil
	}
	return nil, ErrNoVersion
}

func firstVersionMatching(
	versions []hangarVersion,
	platform types.PlatformId,
	requireCompatibility bool,
) *hangarVersion {
	for i := range versions {
		version := &versions[i]
		if !version.HasDownloadForPlatform(platform) {
			continue
		}
		if requireCompatibility && !version.SupportsPlatform(platform) {
			continue
		}
		return version
	}
	return nil
}

func preferredDownloadPlatform(platform types.PlatformId) types.PlatformId {
	if platform == types.PlatformAny || platform == types.PlatformNone || platform == types.PlatformUnknown {
		return types.PlatformId("paper")
	}
	return types.PlatformId("paper")
}
