package hangar

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/log"
)

const hangarAPIBaseURL = "https://hangar.papermc.io/api/v1"

var (
	ErrInvalidAPIResponse = errors.New("hangar: invalid api response")
	ErrNoProject          = errors.New("hangar: project not found")
	ErrNoVersion          = errors.New("hangar: version not found")
	ErrNoDownload         = errors.New("hangar: download not found")
)

func getProject(name catalog.Name) (*hangarProject, error) {
	if project, err := getProjectByPath(string(name)); err == nil {
		if project.ProjectRef().CanonicalName() == name {
			return project, nil
		}
	}

	search, err := searchProjects(string(name), catalog.EcosystemUnspecified)
	if err != nil {
		return nil, err
	}

	for _, project := range search.Result {
		if project.ProjectRef().CanonicalName() == name {
			return getProjectByRef(project.ProjectRef())
		}
	}

	return nil, ErrNoProject
}

func getProjectByRef(ref hangarProjectRef) (*hangarProject, error) {
	if ref.Owner == "" || ref.Slug == "" {
		return nil, ErrNoProject
	}
	return getProjectByPath(ref.LookupPath())
}

func getProjectByPath(path string) (*hangarProject, error) {
	project := &hangarProject{}
	if err := getJSON(hangarProjectURL(path), project); err != nil {
		return nil, err
	}
	return project, nil
}

func searchProjects(
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
	if err := getJSON(hangarProjectsURL(params), res); err != nil {
		return nil, err
	}
	return res, nil
}

func getVersion(id catalog.VersionedRef) (*hangarVersion, error) {
	project, err := getProject(id.Name)
	if err != nil {
		return nil, err
	}

	version := &hangarVersion{}
	if err := getJSON(
		hangarVersionURL(
			project.ProjectRef(),
			id.Version.String(),
		), version,
	); err != nil {
		return nil, err
	}
	return version, nil
}

func listVersions(name catalog.Name) ([]hangarVersion, error) {
	project, err := getProject(name)
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
		if err := getJSON(
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

func getJSON(rawURL string, out any) error {
	log.Debug("hangar request: " + rawURL)
	res, err := cache.CachedGetRequest(
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
