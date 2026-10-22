package curseforge

import (
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

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

// latestFile finds the latest file compatible with local runtime facts and
// the requested selector. stable accepts releases only; beta accepts any
// channel; any prefers a release and falls back to a prerelease.
func latestFile(
	modId int32,
	local upstream.LocalContext,
	platform catalog.Ecosystem,
	selector versionpkg.Version,
) (*fileResponse, error) {
	files, err := listFiles(
		modId,
		curseForgeGameVersion(local),
		modLoaderType(platform),
	)
	if err != nil {
		return nil, err
	}
	latest := selectLatestFile(files, selector)
	if latest == nil {
		return nil, ErrNoCompatibleFile
	}
	if latest.DownloadUrl == nil {
		return nil, ErrDownloadNotAllowed
	}
	return latest, nil
}

func getFileByDisplayName(
	modId int32,
	version string,
	local upstream.LocalContext,
	platform catalog.Ecosystem,
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
	if !local.HasMinecraftVersion() {
		return ""
	}
	return local.MinecraftVersion.String()
}

type modFileDataResponse struct {
	Data fileResponse `json:"data"`
}

func getModFileById(modId, fileId int32) (*fileResponse, error) {
	u := modFileURL(modId, fileId)
	var resp modFileDataResponse
	if err := get(u, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}
