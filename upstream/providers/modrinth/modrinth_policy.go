package modrinth

import (
	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
)

func selectExactVersion(
	versions []*versionResponse,
	id catalog.VersionedRef,
	gameVersion versionpkg.Version,
) *versionResponse {
	for _, version := range versions {
		if versionpkg.Version(version.VersionNumber) == id.Version &&
			versionSupportsLoader(version, id.Ecosystem) &&
			versionSupportsGameVersion(version, gameVersion) {
			return version
		}
	}
	return nil
}

func versionSupportsGameVersion(
	version *versionResponse,
	gameVersion versionpkg.Version,
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

func localGameVersionKnown(gameVersion versionpkg.Version) bool {
	return gameVersion != "" &&
		!gameVersion.IsInvalid() &&
		!gameVersion.CanInfer()
}

// selectLatestVersionForSelector applies issue 60 selector semantics. stable
// accepts release versions only; beta accepts any channel; any prefers a
// release but still falls back to a prerelease.
func selectLatestVersionForSelector(
	versions []*versionResponse,
	platform catalog.Ecosystem,
	gameVersion versionpkg.Version,
	filterByLoader bool,
	selector versionpkg.Version,
) *versionResponse {
	switch selector {
	case versionpkg.Stable:
		return latestReleaseVersion(
			versions,
			platform,
			gameVersion,
			filterByLoader,
		)
	case versionpkg.Beta:
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
	platform catalog.Ecosystem,
	gameVersion versionpkg.Version,
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
	platform catalog.Ecosystem,
	gameVersion versionpkg.Version,
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
