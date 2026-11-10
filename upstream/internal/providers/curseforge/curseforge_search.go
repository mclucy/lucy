package curseforge

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/mclucy/lucy/catalog"
)

const (
	minecraftGameId = 432
	modsClassId     = 6
)

// modLoaderType maps lucy Platform to CurseForge ModLoaderType enum.
// Docs: https://docs.curseforge.com/rest-api/#search-mods
func modLoaderType(p catalog.Ecosystem) int {
	switch p {
	case catalog.EcosystemForge:
		return 1
	case catalog.EcosystemFabric:
		return 4
	case catalog.EcosystemQuilt:
		return 5
	case catalog.EcosystemNeoForge:
		return 6
	default:
		return 0 // Any
	}
}

// modLoaderTypes lists the server-side loader filters that serve an
// ecosystem. Quilt Loader runs Fabric mods, so a Quilt request accepts both
// the Quilt and Fabric loader types; the REST filter takes one value per
// request, so callers union the results. Any always yields exactly one filter.
func modLoaderTypes(p catalog.Ecosystem) []int {
	if p == catalog.EcosystemQuilt {
		return []int{5, 4}
	}
	return []int{modLoaderType(p)}
}

// searchUrl builds a project search: https://docs.curseforge.com/rest-api/#search-mods.
func searchUrl(
	query catalog.Name,
	ecosystem catalog.Ecosystem,
) string {
	return searchUrlForLoader(query, modLoaderType(ecosystem))
}

// searchUrlForLoader builds a project search constrained to one CurseForge
// loader type. A zero loader type searches every loader.
func searchUrlForLoader(query catalog.Name, loader int) string {
	params := url.Values{}
	params.Set("gameId", fmt.Sprintf("%d", minecraftGameId))
	params.Set("classId", fmt.Sprintf("%d", modsClassId))
	params.Set("searchFilter", string(query))
	// sortField=2 maps to Popularity in CurseForge ModsSearchSortField enum;
	// sortOrder=desc is its required direction. See Docs link above.
	params.Set("sortField", "2")
	params.Set("sortOrder", "desc")
	params.Set("pageSize", "50")

	if loader != 0 {
		params.Set("modLoaderType", fmt.Sprintf("%d", loader))
	}

	return baseUrl + "/v1/mods/search?" + params.Encode()
}

// slugSearchUrl builds a slug lookup: https://docs.curseforge.com/rest-api/#search-mods.
func slugSearchUrl(slug catalog.Name) string {
	params := url.Values{}
	params.Set("gameId", fmt.Sprintf("%d", minecraftGameId))
	params.Set("classId", fmt.Sprintf("%d", modsClassId))
	params.Set("slug", string(slug))
	params.Set("pageSize", "50")
	return baseUrl + "/v1/mods/search?" + params.Encode()
}

// modUrl builds a project lookup: https://docs.curseforge.com/rest-api/#get-mod.
func modUrl(modId int32) string {
	return fmt.Sprintf("%s/v1/mods/%d", baseUrl, modId)
}

// modDescriptionUrl builds a description request: https://docs.curseforge.com/rest-api/#get-mod-description.
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

// modFilesUrl builds a file-list request: https://docs.curseforge.com/rest-api/#get-mod-files.
func modFilesUrl(modId int32, gameVersion string, loaderType int) string {
	params := url.Values{}
	params.Set("pageSize", strconv.Itoa(modFilesPageSize))

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

// modFileURL builds a file lookup: https://docs.curseforge.com/rest-api/#get-mod-file.
func modFileURL(modId, fileId int32) string {
	return fmt.Sprintf("%s/v1/mods/%d/files/%d", baseUrl, modId, fileId)
}
