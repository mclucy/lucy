package modrinth

import "github.com/mclucy/lucy/types"

func selectExactVersion(
	versions []*versionResponse,
	id types.VersionedPackageRef,
	gameVersion types.Version,
) *versionResponse {
	for _, version := range versions {
		if types.Version(version.VersionNumber) == id.Version &&
			versionSupportsLoader(version, id.Ecosystem) &&
			versionSupportsGameVersion(version, gameVersion) {
			return version
		}
	}
	return nil
}

func versionSupportsGameVersion(
	version *versionResponse,
	gameVersion types.Version,
) bool {
	if !localGameVersionKnown(gameVersion) {
		return true
	}
	for _, supported := range version.GameVersions {
		if supported == gameVersion.String() {
			return true
		}
	}
	return false
}

func localGameVersionKnown(gameVersion types.Version) bool {
	return gameVersion != "" &&
		!gameVersion.IsInvalid() &&
		!gameVersion.CanInfer()
}

// selectLatestVersionForSelector applies issue 60 selector semantics. stable
// accepts release versions only; beta accepts any channel; any prefers a
// release but still falls back to a prerelease.
func selectLatestVersionForSelector(
	versions []*versionResponse,
	platform types.Ecosystem,
	gameVersion types.Version,
	filterByLoader bool,
	selector types.Version,
) *versionResponse {
	switch selector {
	case types.VersionStable:
		return latestReleaseVersion(
			versions,
			platform,
			gameVersion,
			filterByLoader,
		)
	case types.VersionBeta:
		return latestAnyVersion(versions, platform, gameVersion, filterByLoader)
	default:
		if selected := latestReleaseVersion(
			versions,
			platform,
			gameVersion,
			filterByLoader,
		); selected != nil {
			return selected
		}
		return latestAnyVersion(versions, platform, gameVersion, filterByLoader)
	}
}

func latestReleaseVersion(
	versions []*versionResponse,
	platform types.Ecosystem,
	gameVersion types.Version,
	filterByLoader bool,
) *versionResponse {
	var selected *versionResponse
	for _, version := range versions {
		if (filterByLoader && !versionSupportsLoader(version, platform)) ||
			!versionSupportsGameVersion(version, gameVersion) {
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
	platform types.Ecosystem,
	gameVersion types.Version,
	filterByLoader bool,
) *versionResponse {
	var selected *versionResponse
	for _, version := range versions {
		if (filterByLoader && !versionSupportsLoader(version, platform)) ||
			!versionSupportsGameVersion(version, gameVersion) {
			continue
		}
		if selected == nil || version.DatePublished.After(selected.DatePublished) {
			selected = version
		}
	}
	return selected
}
