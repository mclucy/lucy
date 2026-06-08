package curseforge

import (
	"github.com/mclucy/lucy/slugmap"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"
)

// resolveSlug resolves a project slug to its mod data by searching with the
// slug parameter. CurseForge has no "get by slug" endpoint, so we search with
// the slug query parameter and look for an exact match.
// Docs: https://docs.curseforge.com/rest-api/#search-mods
func resolveSlug(slug types.PackageName) (*modResponse, error) {
	// Canonicalize slug-like name to provider canonical slug before search.
	if canonical, ok := slugmap.Default().GetLoose(
		types.SourceCurseForge,
		string(slug),
	); ok {
		slug = types.PackageName(canonical)
	}

	u := slugSearchUrl(slug)
	var resp searchResponse
	if err := get(u, &resp); err != nil {
		return nil, err
	}

	if len(resp.Data) == 0 {
		return nil, ErrProjectNotFound
	}

	// If exactly one result, use it.
	if len(resp.Data) == 1 {
		return &resp.Data[0], nil
	}

	// Multiple results — find exact slug match.
	for i := range resp.Data {
		if syntax.ToProjectName(resp.Data[i].Slug) == slug {
			return &resp.Data[i], nil
		}
	}

	return nil, ErrAmbiguousSlug
}

// getModById fetches a mod by its numeric ID.
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
