package curseforge

import (
	"strconv"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
)

// resolveProjectID resolves a project identity to a CurseForge mod ID. A
// numeric string is treated as an ID; anything else is resolved as a slug.
func resolveProjectID(project string) (int32, error) {
	if id, err := strconv.ParseInt(project, 10, 32); err == nil {
		return int32(id), nil
	}
	mod, err := resolveSlug(catalog.Name(project))
	if err != nil {
		return 0, err
	}
	return mod.ID, nil
}

// resolveSlug resolves a project slug to its mod data. CurseForge has no
// get-by-slug endpoint, so it searches by slug and requires an exact match.
// Docs: https://docs.curseforge.com/rest-api/#search-mods
func resolveSlug(slug catalog.Name) (*modResponse, error) {
	u := slugSearchUrl(slug)
	var resp searchResponse
	if err := get(u, &resp); err != nil {
		return nil, err
	}

	if len(resp.Data) == 0 {
		return nil, ErrProjectNotFound
	}

	if len(resp.Data) == 1 {
		return &resp.Data[0], nil
	}

	for i := range resp.Data {
		if input.ToProjectName(resp.Data[i].Slug) == slug {
			return &resp.Data[i], nil
		}
	}

	return nil, ErrAmbiguousSlug
}

// Docs: https://docs.curseforge.com/rest-api/#get-mod
func getModById(modId int32) (*modResponse, error) {
	u := modUrl(modId)
	var resp modDataResponse
	if err := get(u, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func getModDescription(modId int32) (string, error) {
	u := modDescriptionUrl(modId, true)
	var resp stringDataResponse
	if err := get(u, &resp); err != nil {
		return "", err
	}
	return resp.Data, nil
}
