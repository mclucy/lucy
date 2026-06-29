package curseforge

import (
	"fmt"
	"net/url"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

const (
	minecraftGameId = 432
	modsClassId     = 6
)

// modLoaderType maps lucy Platform to CurseForge ModLoaderType enum.
// Docs: https://docs.curseforge.com/rest-api/#search-mods
func modLoaderType(p types.Ecosystem) int {
	switch p {
	case types.EcoForge:
		return 1
	case types.EcoFabric:
		return 4
	case types.EcoNeoforge:
		return 6
	default:
		return 0 // Any
	}
}

// searchUrl builds the search URL for the CurseForge /v1/mods/search endpoint.
// Docs: https://docs.curseforge.com/rest-api/#search-mods
func searchUrl(
	query types.BarePackageName,
	options upstream.SearchOptions,
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

	if loader := modLoaderType(options.FilterEcosystem); loader != 0 {
		params.Set("modLoaderType", fmt.Sprintf("%d", loader))
	}

	return baseUrl + "/v1/mods/search?" + params.Encode()
}

// slugSearchUrl builds a URL to find a mod by its exact slug.
// Docs: https://docs.curseforge.com/rest-api/#search-mods
func slugSearchUrl(slug types.BarePackageName) string {
	params := url.Values{}
	params.Set("gameId", fmt.Sprintf("%d", minecraftGameId))
	params.Set("classId", fmt.Sprintf("%d", modsClassId))
	params.Set("slug", string(slug))
	params.Set("pageSize", "50")
	return baseUrl + "/v1/mods/search?" + params.Encode()
}

// modUrl builds the URL for getting a mod by its numeric ID.
// Docs: https://docs.curseforge.com/rest-api/#get-mod
func modUrl(modId int32) string {
	return fmt.Sprintf("%s/v1/mods/%d", baseUrl, modId)
}

// modDescriptionUrl builds the URL for getting a mod's long description.
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

// modFilesUrl builds the URL for listing files of a mod, with optional
// filtering by game version and mod loader.
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

// modFileUrl builds the URL for a single mod file.
// Docs: https://docs.curseforge.com/rest-api/#get-mod-file
func modFileUrl(modId, fileId int32) string {
	return fmt.Sprintf("%s/v1/mods/%d/files/%d", baseUrl, modId, fileId)
}
