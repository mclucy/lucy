package hangar

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
)

const hangarAPIBaseURL = "https://hangar.papermc.io/api/v1"

var (
	ErrInvalidAPIResponse = errors.New("hangar: invalid api response")
	ErrNoProject          = errors.New("hangar: project not found")
	ErrNoVersion          = errors.New("hangar: version not found")
	ErrNoDownload         = errors.New("hangar: download not found")
)

func getProject(name types.ProjectName) (*hangarProject, error) {
	if project, err := getProjectByPath(string(name)); err == nil {
		if project.ProjectRef().CanonicalName() == name {
			return project, nil
		}
	}

	search, err := searchProjects(string(name), types.SearchOptions{})
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

func searchProjects(query string, options types.SearchOptions) (*projectSearchResponse, error) {
	params := url.Values{}
	if query != "" {
		params.Set("query", query)
	}
	params.Set("limit", "25")
	if platform := searchPlatform(options.FilterPlatform); platform != "" {
		params.Set("platform", platform)
	}

	res := &projectSearchResponse{}
	if err := getJSON(hangarProjectsURL(params), res); err != nil {
		return nil, err
	}
	return res, nil
}

func getVersion(id types.PackageId) (*hangarVersion, error) {
	project, err := getProject(id.Name)
	if err != nil {
		return nil, err
	}

	version := &hangarVersion{}
	if err := getJSON(hangarVersionURL(project.ProjectRef(), id.Version.String()), version); err != nil {
		return nil, err
	}
	return version, nil
}

func listVersions(name types.ProjectName) ([]hangarVersion, error) {
	project, err := getProject(name)
	if err != nil {
		return nil, err
	}

	params := url.Values{}
	params.Set("limit", "25")

	res := &HangarVersionListResponse{}
	if err := getJSON(hangarVersionsURL(project.ProjectRef(), params), res); err != nil {
		return nil, err
	}
	if len(res.Result) == 0 {
		return nil, ErrNoVersion
	}
	return res.Result, nil
}

func getJSON(rawURL string, out any) error {
	logger.Debug("hangar request: " + rawURL)
	res, err := http.Get(rawURL)
	if err != nil {
		return fmt.Errorf("hangar: request failed: %w", err)
	}
	defer tools.CloseReader(res.Body, logger.Warn)

	if res.StatusCode == http.StatusNotFound {
		return ErrNoProject
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("hangar: unexpected status %d", res.StatusCode)
	}

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("hangar: failed to read response: %w", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
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

func searchPlatform(platform types.Platform) string {
	switch platform {
	case types.PlatformBukkit:
		return hangarPreferredPlatform
	case types.PlatformAny, types.PlatformNone, types.PlatformUnknown:
		return hangarPreferredPlatform
	default:
		return ""
	}
}
