package curseforge

import (
	"fmt"
	"net/url"
	"slices"
	"strconv"

	"github.com/mclucy/lucy/catalog"
)

const (
	minecraftGameId = 432
	modsClassId     = 6
)

// curseForgeLoaderTypes maps a lucy ecosystem to the CurseForge ModLoaderType
// enum. An ecosystem absent from the table searches every loader.
// Docs: https://docs.curseforge.com/rest-api/#search-mods
var curseForgeLoaderTypes = map[catalog.Ecosystem]int{
	catalog.EcosystemForge:    1,
	catalog.EcosystemFabric:   4,
	catalog.EcosystemQuilt:    5,
	catalog.EcosystemNeoForge: 6,
}

// modLoaderType maps a lucy ecosystem to its CurseForge ModLoaderType enum.
func modLoaderType(p catalog.Ecosystem) int {
	return curseForgeLoaderTypes[p]
}

// modLoaderTypes lists the loader filters that serve a runtime of p, in
// descriptor precedence order. A runtime that loads more than one ecosystem
// (Quilt loads Fabric) needs one request per distinct enum value; the REST
// filter takes one value per request, so callers union the results. Any
// runtime without a mapped loader yields exactly one unfiltered request.
func modLoaderTypes(p catalog.Ecosystem) []int {
	loaders := p.Loads()
	if len(loaders) == 0 {
		return []int{0}
	}
	types := make([]int, 0, len(loaders))
	for _, loader := range loaders {
		loaderType := modLoaderType(loader)
		if slices.Contains(types, loaderType) {
			continue
		}
		types = append(types, loaderType)
	}
	return types
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
