package curseforge

import (
	"fmt"
	"net/url"

	"github.com/mclucy/lucy/types"
)

const (
	minecraftGameId = 432
	modsClassId     = 6
)

// modLoaderType maps lucy Platform to CurseForge ModLoaderType enum.
// Docs: https://docs.curseforge.com/rest-api/#search-mods
func modLoaderType(p types.Ecosystem) int {
	switch p {
	case types.EcosystemForge:
		return 1
	case types.EcosystemFabric:
		return 4
	case types.EcosystemNeoForge:
		return 6
	default:
		return 0 // Any
	}
}

// Docs: https://docs.curseforge.com/rest-api/#search-mods
func searchUrl(
	query types.PackageName,
	ecosystem types.Ecosystem,
) string {
	params := url.Values{}
	params.Set("gameId", fmt.Sprintf("%d", minecraftGameId))
	params.Set("classId", fmt.Sprintf("%d", modsClassId))
	params.Set("searchFilter", string(query))
	// sortField=2 maps to Popularity in CurseForge ModsSearchSortField enum;
	// sortOrder=desc is its required direction. See Docs link above.
	params.Set("sortField", "2")
	params.Set("sortOrder", "desc")
	params.Set("pageSize", "50")

	if loader := modLoaderType(ecosystem); loader != 0 {
		params.Set("modLoaderType", fmt.Sprintf("%d", loader))
	}

	return baseUrl + "/v1/mods/search?" + params.Encode()
}

// Docs: https://docs.curseforge.com/rest-api/#search-mods
func slugSearchUrl(slug types.PackageName) string {
	params := url.Values{}
	params.Set("gameId", fmt.Sprintf("%d", minecraftGameId))
	params.Set("classId", fmt.Sprintf("%d", modsClassId))
	params.Set("slug", string(slug))
	params.Set("pageSize", "50")
	return baseUrl + "/v1/mods/search?" + params.Encode()
}

// Docs: https://docs.curseforge.com/rest-api/#get-mod
func modUrl(modId int32) string {
	return fmt.Sprintf("%s/v1/mods/%d", baseUrl, modId)
}

// Docs: https://docs.curseforge.com/rest-api/#get-mod-description
func modDescriptionUrl(modId int32, stripped bool) string {
	params := url.Values{}
	if stripped {
		params.Set("stripped", "true")
	}

	u := fmt.Sprintf("%s/v1/mods/%d/description", baseUrl, modId)
	if len(params) == 0 {
		return u
	}
	return u + "?" + params.Encode()
}

// Docs: https://docs.curseforge.com/rest-api/#get-mod-files
func modFilesUrl(modId int32, gameVersion string, loaderType int) string {
	params := url.Values{}
	params.Set("pageSize", "50")

	if gameVersion != "" {
		params.Set("gameVersion", gameVersion)
	}
	if loaderType != 0 {
		params.Set("modLoaderType", fmt.Sprintf("%d", loaderType))
	}

	return fmt.Sprintf(
		"%s/v1/mods/%d/files?%s",
		baseUrl,
		modId,
		params.Encode(),
	)
}

// Docs: https://docs.curseforge.com/rest-api/#get-mod-file
func modFileURL(modId, fileId int32) string {
	return fmt.Sprintf("%s/v1/mods/%d/files/%d", baseUrl, modId, fileId)
}
