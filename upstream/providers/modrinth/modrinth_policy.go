package modrinth

import "github.com/mclucy/lucy/types"

func selectExactVersion(
	versions []*versionResponse,
	id types.VersionedPackageRef,
	gameVersion types.BareVersion,
) *versionResponse {
	for _, version := range versions {
		if types.BareVersion(version.VersionNumber) == id.Version &&
			versionSupportsLoader(version, id.Eco) &&
			versionSupportsGameVersion(version, gameVersion) {
			return version
		}
	}
	return nil
}

func versionSupportsGameVersion(
	version *versionResponse,
	gameVersion types.BareVersion,
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

func localGameVersionKnown(gameVersion types.BareVersion) bool {
	return gameVersion != "" &&
		!gameVersion.IsInvalid() &&
		!gameVersion.CanInfer()
}

func selectLatestVersionCandidate(
	versions []*versionResponse,
	platform types.Ecosystem,
	gameVersion types.BareVersion,
) (*versionResponse, bool) {
	return selectLatestVersionByLoader(versions, platform, gameVersion, false)
}

func selectLatestCompatibleVersionCandidate(
	versions []*versionResponse,
	platform types.Ecosystem,
	gameVersion types.BareVersion,
) (*versionResponse, bool) {
	return selectLatestVersionByLoader(versions, platform, gameVersion, true)
}

func selectLatestVersionByLoader(
	versions []*versionResponse,
	platform types.Ecosystem,
	gameVersion types.BareVersion,
	filterByLoader bool,
) (*versionResponse, bool) {
	selected := latestReleaseVersion(
		versions,
		platform,
		gameVersion,
		filterByLoader,
	)
	if selected != nil {
		return selected, false
	}
	return latestAnyVersion(versions, platform, gameVersion, filterByLoader), true
}

func latestReleaseVersion(
	versions []*versionResponse,
	platform types.Ecosystem,
	gameVersion types.BareVersion,
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
	gameVersion types.BareVersion,
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
