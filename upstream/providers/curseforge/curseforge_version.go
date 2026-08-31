package curseforge

import (
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
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

// latestFile finds the latest release file compatible with local runtime facts.
func latestFile(
	modId int32,
	local upstream.LocalContext,
	platform types.Ecosystem,
) (*fileResponse, error) {
	files, err := listFiles(
		modId,
		curseForgeGameVersion(local),
		modLoaderType(platform),
	)
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

func latestCompatibleFile(
	modId int32,
	local upstream.LocalContext,
	platform types.Ecosystem,
) (*fileResponse, error) {
	return latestFile(modId, local, platform)
}

func getFileByDisplayName(
	modId int32,
	version string,
	local upstream.LocalContext,
	platform types.Ecosystem,
) (*fileResponse, error) {
	files, err := listFiles(
		modId,
		curseForgeGameVersion(local),
		modLoaderType(platform),
	)
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

func curseForgeGameVersion(local upstream.LocalContext) string {
	if !local.HasGameVersion() {
		return ""
	}
	return local.GameVersion.String()
}

type modFileDataResponse struct {
	Data fileResponse `json:"data"`
}

func getModFileById(modId, fileId int32) (*fileResponse, error) {
	u := modFileUrl(modId, fileId)
	var resp modFileDataResponse
	if err := get(u, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}
