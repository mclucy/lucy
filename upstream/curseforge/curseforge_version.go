package curseforge

import (
	"github.com/mclucy/lucy/types"
)

// listFiles fetches files for a mod with optional filtering by game version
// and mod loader type.
// Docs: https://docs.curseforge.com/rest-api/#get-mod-files
func listFiles(modId int32, gameVersion string, loaderType int) (
	[]fileResponse, error,
) {
	u := modFilesUrl(modId, gameVersion, loaderType)
	var resp filesResponse
	if err := get(u, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// latestFile finds the latest release file for a mod (no version/platform
// filtering).
func latestFile(modId int32) (*fileResponse, error) {
	files, err := listFiles(modId, "", 0)
	if err != nil {
		return nil, err
	}
	latest := selectLatestReleaseFile(files)

	if latest == nil {
		return nil, ErrNoCompatibleFile
	}
	if latest.DownloadUrl == nil {
		return nil, ErrDownloadNotAllowed
	}
	return latest, nil
}

// latestCompatibleFile finds the latest release file compatible with the
// current server's game version and platform.
func latestCompatibleFile(modId int32, platform types.Platform) (
	*fileResponse, error,
) {
	// Platform inference removed to avoid circular imports.
	// Caller should provide explicit platform or this will use latest.
	_ = platform
	files, err := listFiles(modId, "", 0)
	if err != nil {
		return nil, err
	}
	latest := selectLatestReleaseFile(files)

	if latest == nil {
		return nil, ErrNoCompatibleFile
	}
	if latest.DownloadUrl == nil {
		return nil, ErrDownloadNotAllowed
	}
	return latest, nil
}

// getFileByDisplayName finds a file matching a specific version string.
// It checks DisplayName and FileName for a match.
func getFileByDisplayName(
	modId int32,
	version string,
	platform types.Platform,
) (*fileResponse, error) {
	loaderType := modLoaderType(platform)
	files, err := listFiles(modId, "", loaderType)
	if err != nil {
		return nil, err
	}
	selected := selectFileByVersion(files, version)
	if selected == nil {
		return nil, ErrNoCompatibleFile
	}
	if selected.DownloadUrl == nil {
		return nil, ErrDownloadNotAllowed
	}
	return selected, nil
}
