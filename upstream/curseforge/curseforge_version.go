package curseforge

import (
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/probe"
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

	var latest *fileResponse
	for i := range files {
		f := &files[i]
		if !f.IsAvailable || f.ReleaseType != 1 { // 1 = Release
			continue
		}
		if latest == nil || f.FileDate > latest.FileDate {
			latest = f
		}
	}

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
	serverInfo := probe.ServerInfo()
	if !serverInfo.Runtime.IsValid() {
		logger.Info(
			"no valid server, unable to infer a compatible version. " +
			"falling back to latest version",
		)
		return latestFile(modId)
	}

	gameVersion := serverInfo.Runtime.GameVersion.String()
	loaderType := modLoaderType(platform)

	files, err := listFiles(modId, gameVersion, loaderType)
	if err != nil {
		return nil, err
	}

	var latest *fileResponse
	for i := range files {
		f := &files[i]
		if !f.IsAvailable || f.ReleaseType != 1 {
			continue
		}
		if latest == nil || f.FileDate > latest.FileDate {
			latest = f
		}
	}

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

	for i := range files {
		f := &files[i]
		if !f.IsAvailable {
			continue
		}
		if f.DisplayName == version || f.FileName == version {
			if f.DownloadUrl == nil {
				return nil, ErrDownloadNotAllowed
			}
			return f, nil
		}
	}

	return nil, ErrNoCompatibleFile
}
