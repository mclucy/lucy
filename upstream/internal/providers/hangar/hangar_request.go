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
	"github.com/mclucy/lucy/input"
)

const hangarAPIBaseURL = "https://hangar.papermc.io/api/v1"

var (
	ErrInvalidAPIResponse = errors.New("hangar: invalid api response")
	ErrNoProject          = errors.New("hangar: project not found")
	ErrNoVersion          = errors.New("hangar: version not found")
	ErrNoDownload         = errors.New("hangar: download not found")
	ErrAmbiguousProject   = errors.New("hangar: project name is ambiguous")
)

// getProject resolves one explicit project identity. A numeric Hangar project
// ID is the stable coordinate; an owner/slug path is accepted and verified
// against the response because Hangar itself resolves the slug without
// checking the owner segment. A bare slug is accepted for older references and
// is resolved to exactly one project or rejected as ambiguous.
func (p *Provider) getProject(
	ctx context.Context,
	name catalog.Name,
) (*hangarProject, error) {
	raw := strings.TrimSpace(string(name))
	if raw == "" {
		return nil, ErrNoProject
	}

	if id, ok := hangarNumericProjectID(raw); ok {
		project, err := p.getProjectByPath(ctx, strconv.FormatInt(id, 10))
		if err != nil {
			return nil, err
		}
		if project.ID != id {
			return nil, ErrNoProject
		}
		return project, nil
	}

	if owner, slug, ok := splitHangarProjectPath(raw); ok {
		return p.getProjectByRef(
			ctx,
			hangarProjectRef{Owner: owner, Slug: slug},
		)
	}

	project, err := p.getProjectByPath(ctx, raw)
	if err != nil && !errors.Is(err, ErrNoProject) {
		return nil, err
	}
	if err == nil && strings.EqualFold(project.Namespace.Slug, raw) {
		return project, nil
	}
	return p.searchProjectByName(ctx, name)
}

func (p *Provider) getProjectByRef(
	ctx context.Context,
	ref hangarProjectRef,
) (*hangarProject, error) {
	if ref.Owner == "" || ref.Slug == "" {
		return nil, ErrNoProject
	}
	project, err := p.getProjectByPath(ctx, ref.LookupPath())
	if err != nil {
		return nil, err
	}
	// Hangar resolves the slug even when the owner segment is wrong, so the
	// response must be checked before it becomes a stable identity.
	resolved := project.ProjectRef()
	if !strings.EqualFold(resolved.Owner, ref.Owner) ||
		!strings.EqualFold(resolved.Slug, ref.Slug) {
		return nil, ErrNoProject
	}
	return project, nil
}

func (p *Provider) getProjectByPath(
	ctx context.Context,
	lookupPath string,
) (*hangarProject, error) {
	lookupPath = strings.Trim(strings.TrimSpace(lookupPath), "/")
	if lookupPath == "" {
		return nil, ErrNoProject
	}
	project := &hangarProject{}
	if err := p.getJSON(ctx, hangarProjectURL(lookupPath), project); err != nil {
		return nil, err
	}
	return project, nil
}

// searchProjectByName resolves a display name only when exactly one project
// shares its normalized slug. Picking the first search hit would make the
// identity depend on result ordering.
func (p *Provider) searchProjectByName(
	ctx context.Context,
	name catalog.Name,
) (*hangarProject, error) {
	search, err := p.searchProjects(
		ctx,
		string(name),
		catalog.EcosystemUnspecified,
	)
	if err != nil {
		return nil, err
	}

	canonical := input.ToProjectName(string(name))
	var matches []hangarProject
	seen := make(map[int64]struct{})
	for _, project := range search.Result {
		if project.ProjectRef().CanonicalName() != canonical {
			continue
		}
		if project.ID > 0 {
			if _, duplicate := seen[project.ID]; duplicate {
				continue
			}
			seen[project.ID] = struct{}{}
		}
		matches = append(matches, project)
	}

	switch len(matches) {
	case 0:
		return nil, ErrNoProject
	case 1:
		return p.getProjectByRef(ctx, matches[0].ProjectRef())
	default:
		return nil, ErrAmbiguousProject
	}
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
			hangarProjectLookup(project),
			id.Version.String(),
		), version,
	); err != nil {
		if errors.Is(err, ErrNoProject) {
			return nil, ErrNoVersion
		}
		return nil, err
	}
	if version.ProjectID != 0 && project.ID != 0 &&
		version.ProjectID != project.ID {
		return nil, ErrInvalidAPIResponse
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
	return p.listProjectVersions(ctx, project)
}

func (p *Provider) listProjectVersions(
	ctx context.Context,
	project *hangarProject,
) ([]hangarVersion, error) {
	lookup := hangarProjectLookup(project)
	if lookup == "" {
		return nil, ErrNoProject
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
			hangarVersionsURL(lookup, params),
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

// hangarProjectLookup prefers the numeric project ID: Hangar project IDs are
// immutable while owner and slug can change.
func hangarProjectLookup(project *hangarProject) string {
	if project == nil {
		return ""
	}
	if project.ID > 0 {
		return strconv.FormatInt(project.ID, 10)
	}
	return project.ProjectRef().LookupPath()
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

func hangarProjectURL(lookupPath string) string {
	segments := strings.Split(lookupPath, "/")
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}
	return hangarAPIBaseURL + "/projects/" + strings.Join(segments, "/")
}

func hangarVersionsURL(lookupPath string, params url.Values) string {
	return withQuery(hangarProjectURL(lookupPath)+"/versions", params)
}

func hangarVersionURL(lookupPath, version string) string {
	return hangarProjectURL(lookupPath) + "/versions/" + url.PathEscape(version)
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
		return hangarPlatformPaper
	case catalog.EcosystemBungeeCord:
		return hangarPlatformWaterfall
	case catalog.EcosystemVelocity:
		return hangarPlatformVelocity
	default:
		return ""
	}
}

func hangarNumericProjectID(raw string) (int64, bool) {
	if raw == "" {
		return 0, false
	}
	for _, character := range raw {
		if character < '0' || character > '9' {
			return 0, false
		}
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func splitHangarProjectPath(raw string) (owner, slug string, ok bool) {
	parts := strings.Split(raw, "/")
	if len(parts) != 2 {
		return "", "", false
	}
	owner = strings.TrimSpace(parts[0])
	slug = strings.TrimSpace(parts[1])
	if owner == "" || slug == "" {
		return "", "", false
	}
	return owner, slug, true
}
