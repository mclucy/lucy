package modrinth

import "github.com/mclucy/lucy/types"

func selectExactVersion(
	versions []*versionResponse,
	id types.VersionedPackageRef,
) *versionResponse {
	for _, version := range versions {
		if types.BareVersion(version.VersionNumber) == id.Version &&
			versionSupportsLoader(version, id.Platform) {
			return version
		}
	}
	return nil
}

func selectLatestVersionCandidate(
	versions []*versionResponse,
	platform types.PlatformId,
) (*versionResponse, bool) {
	return selectLatestVersionByLoader(versions, platform, false)
}

func selectLatestCompatibleVersionCandidate(
	versions []*versionResponse,
	platform types.PlatformId,
) (*versionResponse, bool) {
	return selectLatestVersionByLoader(versions, platform, true)
}

func selectLatestVersionByLoader(
	versions []*versionResponse,
	platform types.PlatformId,
	filterByLoader bool,
) (*versionResponse, bool) {
	selected := latestReleaseVersion(versions, platform, filterByLoader)
	if selected != nil {
		return selected, false
	}
	return latestAnyVersion(versions, platform, filterByLoader), true
}

func latestReleaseVersion(
	versions []*versionResponse,
	platform types.PlatformId,
	filterByLoader bool,
) *versionResponse {
	var selected *versionResponse
	for _, version := range versions {
		if filterByLoader && !versionSupportsLoader(version, platform) {
			continue
		}
		if version.VersionType == "release" &&
			(selected == nil || version.DatePublished.After(selected.DatePublished)) {
			selected = version
		}
	}
	return selected
}

func latestAnyVersion(
	versions []*versionResponse,
	platform types.PlatformId,
	filterByLoader bool,
) *versionResponse {
	var selected *versionResponse
	for _, version := range versions {
		if filterByLoader && !versionSupportsLoader(version, platform) {
			continue
		}
		if selected == nil || version.DatePublished.After(selected.DatePublished) {
			selected = version
		}
	}
	return selected
}
