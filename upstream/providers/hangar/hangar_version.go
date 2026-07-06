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

func (h *hangarDependencies) ToPackageDependencies() types.PackageDependencies {
	result := types.PackageDependencies{Authentic: true}
	for _, dep := range h.version.DependenciesForPlatform(h.platform) {
		if dep.Name == "" || dep.ExternalURL != nil {
			continue
		}
		result.Value = append(
			result.Value, types.Dependency{
				Id: types.VersionedPackageRef{
					PackageRef: types.PackageRef{
						Eco:  types.EcoUnspecified,
						Name: input.ToProjectName(dep.Name),
					},
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
	case types.VersionAny, types.VersionBeta, types.VersionNone:
		return selectLatestVersion(versions, id.Eco)
	case types.VersionStable:
		return selectLatestCompatibleVersion(versions, id.Eco)
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
	platform types.Ecosystem,
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
		types.EcoUnspecified,
		false,
	); version != nil {
		return version, nil
	}
	return nil, ErrNoVersion
}

func selectLatestCompatibleVersion(
	versions []hangarVersion,
	platform types.Ecosystem,
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
	platform types.Ecosystem,
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

func preferredDownloadPlatform(platform types.Ecosystem) types.Ecosystem {
	if platform == types.EcoUnspecified {
		return "paper"
	}
	return "paper"
}
