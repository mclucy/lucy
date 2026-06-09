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
func modLoaderType(p types.PlatformId) int {
	switch p {
	case types.PlatformForge:
		return 1
	case types.PlatformFabric:
		return 4
	case types.PlatformNeoforge:
		return 6
	default:
		return 0 // Any
	}
}

// curseforgeSearchSortField maps lucy SearchSort to CurseForge
// ModsSearchSortField enum.
// Docs: https://docs.curseforge.com/rest-api/#search-mods
func curseforgeSearchSortField(sort types.SearchSort) int {
	switch sort {
	case types.SearchSortRelevance:
		return 2 // Popularity
	case types.SearchSortDownloads:
		return 6 // TotalDownloads
	case types.SearchSortNewest:
		return 11 // ReleasedDate
	case types.SearchSortName:
		return 4 // Name
	default:
		return 2 // Popularity
	}
}

// searchSortOrder returns the sort order string for the given sort.
func searchSortOrder(sort types.SearchSort) string {
	if sort == types.SearchSortName {
		return "asc"
	}
	return "desc"
}

// searchUrl builds the search URL for the CurseForge /v1/mods/search endpoint.
// Docs: https://docs.curseforge.com/rest-api/#search-mods
func searchUrl(
	query types.BarePackageName,
	options types.SearchOptions,
) string {
	params := url.Values{}
	params.Set("gameId", fmt.Sprintf("%d", minecraftGameId))
	params.Set("classId", fmt.Sprintf("%d", modsClassId))
	params.Set("searchFilter", string(query))
	params.Set(
		"sortField",
		fmt.Sprintf("%d", curseforgeSearchSortField(options.SortBy)),
	)
	params.Set("sortOrder", searchSortOrder(options.SortBy))
	params.Set("pageSize", "50")

	if loader := modLoaderType(options.FilterPlatform); loader != 0 {
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
