package spiget

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
)

func requestJSON(requestURL string, out any, notFound error) error {
	logger.Debug("spiget api: GET " + requestURL)

	resp, err := http.Get(requestURL)
	if err != nil {
		return fmt.Errorf("spiget: request failed: %w", err)
	}
	defer tools.CloseReader(resp.Body, logger.Warn)

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound && notFound != nil {
			return notFound
		}
		return unexpectedStatusError(requestURL, resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("spiget: failed to read response: %w", err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("spiget: failed to decode response: %w", err)
	}
	return nil
}

func searchResources(query string, options types.SearchOptions) (searchResponse, error) {
	u := searchResourcesURL(query, options)
	resp := searchResponse{}
	if err := requestJSON(u, &resp, nil); err != nil {
		return nil, err
	}
	return resp, nil
}

func getResource(id int64) (*resourceResponse, error) {
	resp := &resourceResponse{}
	if err := requestJSON(resourceURL(id), resp, ErrNoProject); err != nil {
		return nil, err
	}
	return resp, nil
}

func getLatestVersion(resourceID int64) (*versionResponse, error) {
	resp := &versionResponse{}
	if err := requestJSON(latestVersionURL(resourceID), resp, ErrNoVersion); err != nil {
		return nil, err
	}
	return resp, nil
}

func listVersions(resourceID int64) ([]versionResponse, error) {
	resp := []versionResponse{}
	if err := requestJSON(versionsURL(resourceID), &resp, ErrNoVersion); err != nil {
		return nil, err
	}
	return resp, nil
}

func searchResourcesURL(query string, options types.SearchOptions) string {
	values := url.Values{}
	values.Set("size", "20")
	if sort := spigetSearchSort(options.SortBy); sort != "" {
		values.Set("sort", sort)
	}
	return spigetAPIBaseURL + "/search/resources/" + url.PathEscape(query) + "?" + values.Encode()
}

func resourceURL(id int64) string {
	return spigetAPIBaseURL + "/resources/" + strconv.FormatInt(id, 10)
}

func latestVersionURL(resourceID int64) string {
	return resourceURL(resourceID) + "/versions/latest"
}

func versionsURL(resourceID int64) string {
	values := url.Values{}
	values.Set("size", "1000")
	values.Set("sort", "-releaseDate")
	return resourceURL(resourceID) + "/versions?" + values.Encode()
}

func spigetSearchSort(sort types.SearchSort) string {
	switch sort {
	case types.SearchSortDownloads:
		return "-downloads"
	case types.SearchSortNewest:
		return "-updateDate"
	case types.SearchSortName:
		return "+name"
	default:
		return ""
	}
}

func parseNumericResourceID(name types.ProjectName) (int64, bool) {
	trimmed := strings.TrimSpace(name.String())
	if trimmed == "" {
		return 0, false
	}
	id, err := strconv.ParseInt(trimmed, 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}
