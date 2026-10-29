package spiget

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
)

func (p *Provider) requestJSON(
	ctx context.Context,
	requestURL string,
	out any,
	notFound error,
) error {
	p.logger.Debug("spiget api: GET " + requestURL)

	resp, err := p.network.CachedGetRequest(
		ctx,
		requestURL,
		cache.BytesRequestOptions{Kind: cache.KindMetadata},
	)
	if err != nil {
		return fmt.Errorf("spiget: request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusNotFound && notFound != nil {
			return notFound
		}
		return unexpectedStatusError(requestURL, resp.StatusCode)
	}

	if err := json.Unmarshal(resp.Data, out); err != nil {
		return fmt.Errorf("spiget: failed to decode response: %w", err)
	}
	return nil
}

func (p *Provider) searchResources(
	ctx context.Context,
	query string,
) (
	searchResponse,
	error,
) {
	u := searchResourcesURL(query)
	resp := searchResponse{}
	if err := p.requestJSON(ctx, u, &resp, nil); err != nil {
		return nil, err
	}
	return resp, nil
}

func (p *Provider) getResource(
	ctx context.Context,
	id int64,
) (*resourceResponse, error) {
	resp := &resourceResponse{}
	if err := p.requestJSON(ctx, resourceURL(id), resp, ErrNoProject); err != nil {
		return nil, err
	}
	return resp, nil
}

func (p *Provider) getLatestVersion(
	ctx context.Context,
	resourceID int64,
) (*versionResponse, error) {
	resp := &versionResponse{}
	if err := p.requestJSON(
		ctx,
		latestVersionURL(resourceID),
		resp,
		ErrNoVersion,
	); err != nil {
		return nil, err
	}
	return resp, nil
}

const spigetVersionPageSize = 1000

func (p *Provider) listVersions(
	ctx context.Context,
	resourceID int64,
) ([]versionResponse, error) {
	var versions []versionResponse
	for page := 1; ; page++ {
		var resp []versionResponse
		if err := p.requestJSON(
			ctx,
			versionsURL(resourceID, page),
			&resp,
			ErrNoVersion,
		); err != nil {
			return nil, err
		}

		versions = append(versions, resp...)
		if len(resp) < spigetVersionPageSize {
			break
		}
	}
	return versions, nil
}

func searchResourcesURL(query string) string {
	values := url.Values{}
	values.Set("size", "20")
	return spigetAPIBaseURL + "/search/resources/" + url.PathEscape(query) + "?" + values.Encode()
}

func resourceURL(id int64) string {
	return spigetAPIBaseURL + "/resources/" + strconv.FormatInt(id, 10)
}

func latestVersionURL(resourceID int64) string {
	return resourceURL(resourceID) + "/versions/latest"
}

func versionsURL(resourceID int64, page int) string {
	values := url.Values{}
	values.Set("size", strconv.Itoa(spigetVersionPageSize))
	values.Set("page", strconv.Itoa(page))
	values.Set("sort", "-releaseDate")
	return resourceURL(resourceID) + "/versions?" + values.Encode()
}

func parseNumericResourceID(name catalog.Name) (int64, bool) {
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
