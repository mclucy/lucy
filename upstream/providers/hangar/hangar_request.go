package hangar

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
)

const hangarAPIBaseURL = "https://hangar.papermc.io/api/v1"

var (
	ErrInvalidAPIResponse = errors.New("hangar: invalid api response")
	ErrNoProject          = errors.New("hangar: project not found")
	ErrNoVersion          = errors.New("hangar: version not found")
	ErrNoDownload         = errors.New("hangar: download not found")
)

func (p *Provider) getProject(
	ctx context.Context,
	name catalog.Name,
) (*hangarProject, error) {
	if project, err := p.getProjectByPath(ctx, string(name)); err == nil {
		if project.ProjectRef().CanonicalName() == name {
			return project, nil
		}
	}

	search, err := p.searchProjects(
		ctx,
		string(name),
		catalog.EcosystemUnspecified,
	)
	if err != nil {
		return nil, err
	}

	for _, project := range search.Result {
		if project.ProjectRef().CanonicalName() == name {
			return p.getProjectByRef(ctx, project.ProjectRef())
		}
	}

	return nil, ErrNoProject
}

func (p *Provider) getProjectByRef(
	ctx context.Context,
	ref hangarProjectRef,
) (*hangarProject, error) {
	if ref.Owner == "" || ref.Slug == "" {
		return nil, ErrNoProject
	}
	return p.getProjectByPath(ctx, ref.LookupPath())
}

func (p *Provider) getProjectByPath(
	ctx context.Context,
	path string,
) (*hangarProject, error) {
	project := &hangarProject{}
	if err := p.getJSON(ctx, hangarProjectURL(path), project); err != nil {
		return nil, err
	}
	return project, nil
}

func (p *Provider) searchProjects(
	ctx context.Context,
	query string,
	ecosystem catalog.Ecosystem,
) (*projectSearchResponse, error) {
	params := url.Values{}
	if query != "" {
		params.Set("query", query)
	}
	params.Set("limit", "25")
	if eco := searchEcosystem(ecosystem); eco != "" {
		params.Set("platform", eco)
	}

	res := &projectSearchResponse{}
	if err := p.getJSON(ctx, hangarProjectsURL(params), res); err != nil {
		return nil, err
	}
	return res, nil
}

func (p *Provider) getVersion(
	ctx context.Context,
	id catalog.VersionedRef,
) (*hangarVersion, error) {
	project, err := p.getProject(ctx, id.Name)
	if err != nil {
		return nil, err
	}

	version := &hangarVersion{}
	if err := p.getJSON(
		ctx,
		hangarVersionURL(
			project.ProjectRef(),
			id.Version.String(),
		), version,
	); err != nil {
		return nil, err
	}
	return version, nil
}

func (p *Provider) listVersions(
	ctx context.Context,
	name catalog.Name,
) ([]hangarVersion, error) {
	project, err := p.getProject(ctx, name)
	if err != nil {
		return nil, err
	}

	// Hangar caps a versions page at 25 entries, so walk every page to keep
	// both resolution and build listing complete.
	const pageSize = 25
	var versions []hangarVersion
	for offset := 0; ; {
		params := url.Values{}
		params.Set("limit", strconv.Itoa(pageSize))
		params.Set("offset", strconv.Itoa(offset))

		res := &VersionListResponse{}
		if err := p.getJSON(
			ctx,
			hangarVersionsURL(project.ProjectRef(), params),
			res,
		); err != nil {
			return nil, err
		}

		versions = append(versions, res.Result...)
		offset += len(res.Result)
		if len(res.Result) == 0 || offset >= res.Pagination.Count {
			break
		}
	}

	if len(versions) == 0 {
		return nil, ErrNoVersion
	}
	return versions, nil
}

func (p *Provider) getJSON(
	ctx context.Context,
	rawURL string,
	out any,
) error {
	p.logger.Debug("hangar request: " + rawURL)
	res, err := p.network.CachedGetRequest(
		ctx,
		rawURL,
		cache.BytesRequestOptions{Kind: cache.KindMetadata},
	)
	if err != nil {
		return fmt.Errorf("hangar: request failed: %w", err)
	}

	if res.StatusCode == http.StatusNotFound {
		return ErrNoProject
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("hangar: unexpected status %d", res.StatusCode)
	}

	if err := json.Unmarshal(res.Data, out); err != nil {
		return fmt.Errorf("hangar: failed to decode response: %w", err)
	}
	return nil
}

func hangarProjectsURL(params url.Values) string {
	return withQuery(hangarAPIBaseURL+"/projects", params)
}

func hangarProjectURL(path string) string {
	return hangarAPIBaseURL + "/projects/" + strings.TrimPrefix(path, "/")
}

func hangarVersionsURL(ref hangarProjectRef, params url.Values) string {
	return withQuery(hangarProjectURL(ref.LookupPath())+"/versions", params)
}

func hangarVersionURL(ref hangarProjectRef, version string) string {
	return hangarProjectURL(ref.LookupPath()) + "/versions/" + url.PathEscape(version)
}

func withQuery(base string, params url.Values) string {
	if len(params) == 0 {
		return base
	}
	return base + "?" + params.Encode()
}

func searchEcosystem(ecosystem catalog.Ecosystem) string {
	switch ecosystem {
	case catalog.EcosystemBukkit, catalog.EcosystemPaper:
		return hangarPreferredPlatform
	case catalog.EcosystemUnspecified:
		return hangarPreferredPlatform
	default:
		return ""
	}
}
