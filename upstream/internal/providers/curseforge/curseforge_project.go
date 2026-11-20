package curseforge

import (
	"context"
	"strconv"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
)

// lookupBatchSize bounds one array-lookup request. CurseForge documents no cap
// for these arrays; a fixed batch keeps each response inside
// maxLookupResponseBytes and makes the request count independent of how many
// files a project publishes.
const lookupBatchSize = 100

// resolveProjectID resolves a project identity to a CurseForge mod ID. A
// numeric string is treated as an ID; anything else is resolved as a slug.
func (p *Provider) resolveProjectID(
	ctx context.Context,
	project string,
) (int32, error) {
	if id, err := strconv.ParseInt(project, 10, 32); err == nil {
		return int32(id), nil
	}
	mod, err := p.resolveSlug(ctx, catalog.Name(project))
	if err != nil {
		return 0, err
	}
	return mod.ID, nil
}

// resolveSlug resolves a project slug to its mod data. CurseForge has no
// get-by-slug endpoint, so it searches by slug and requires an exact match.
// Docs: https://docs.curseforge.com/rest-api/#search-mods
func (p *Provider) resolveSlug(
	ctx context.Context,
	slug catalog.Name,
) (*modResponse, error) {
	u := slugSearchUrl(slug)
	var resp searchResponse
	if err := p.get(ctx, u, &resp); err != nil {
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
func (p *Provider) getModById(
	ctx context.Context,
	modId int32,
) (*modResponse, error) {
	u := modUrl(modId)
	var resp modDataResponse
	if err := p.get(ctx, u, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// getModsByIds returns many projects in one POST /v1/mods request. Projects
// the API does not know are omitted; a 404 response means the same, not a
// failed lookup. Every returned project's ID is its own identity.
// Docs: https://docs.curseforge.com/rest-api/#get-mods
func (p *Provider) getModsByIds(
	ctx context.Context,
	modIDs []int32,
) ([]modResponse, error) {
	if len(modIDs) == 0 {
		return nil, nil
	}
	payload := struct {
		ModIds []int32 `json:"modIds"`
	}{ModIds: modIDs}
	var resp modsDataResponse
	if err := p.readPost(ctx, baseUrl+"/v1/mods", payload, &resp); err != nil {
		if isNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return resp.Data, nil
}

func (p *Provider) getModDescription(
	ctx context.Context,
	modId int32,
) (string, error) {
	u := modDescriptionUrl(modId, true)
	var resp stringDataResponse
	if err := p.get(ctx, u, &resp); err != nil {
		return "", err
	}
	return resp.Data, nil
}
