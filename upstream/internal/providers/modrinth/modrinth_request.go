package modrinth

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"slices"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/internal/fsutil"
)

// Modrinth documents no batch cap. One hundred keeps request URLs short and
// each response bounded; the API accepts this size.
const batchSize = 100

// Modrinth requires a distinctive User-Agent on every request.
const modrinthUserAgent = "lucy/1.0 (+https://github.com/mclucy/lucy)"

// metadataRequestHeaders is shared read-only by every GET.
var metadataRequestHeaders = http.Header{
	"User-Agent": []string{modrinthUserAgent},
}

const (
	versionFilesUrl        = "https://api.modrinth.com/v2/version_files"
	versionsByIdsUrlPrefix = "https://api.modrinth.com/v2/versions?ids="
	projectsByIdsUrlPrefix = "https://api.modrinth.com/v2/projects?ids="
)

// maxBatchResponseBytes bounds one batch response body. A batch of 100 version
// objects with long changelogs stays well under it; the limit only stops a
// runaway body.
const maxBatchResponseBytes = 16 * 1024 * 1024

// versionFilesRequest is the body for POST /v2/version_files.
type versionFilesRequest struct {
	Hashes    []string `json:"hashes"`
	Algorithm string   `json:"algorithm"`
}

// versionFilesByHash maps each SHA-1 digest Modrinth knows to its version.
// Hashes the API does not know are omitted, not reported as errors.
func (p *Provider) versionFilesByHash(
	ctx context.Context,
	hashes []string,
) (map[string]versionResponse, error) {
	body, err := json.Marshal(versionFilesRequest{
		Hashes:    hashes,
		Algorithm: "sha1",
	})
	if err != nil {
		return nil, fmt.Errorf("modrinth: encode hash lookup: %w", err)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		versionFilesUrl,
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("modrinth: create hash lookup request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", modrinthUserAgent)

	p.logger.Debug("modrinth api: POST " + versionFilesUrl)

	//nolint:bodyclose // CloseReader closes the response and logs close errors.
	resp, err := p.network.Do(req)
	if err != nil {
		return nil, fmt.Errorf("modrinth: request failed: %w", err)
	}
	defer fn.CloseReader(resp.Body, p.logger.Warn)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf(
			"%w: status %d",
			ErrInvalidAPIResponse,
			resp.StatusCode,
		)
	}
	raw, err := fsutil.CopyBytes(resp.Body, maxBatchResponseBytes)
	if err != nil {
		return nil, fmt.Errorf("modrinth: failed to read response: %w", err)
	}
	out := make(map[string]versionResponse, len(hashes))
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("modrinth: failed to parse response: %w", err)
	}
	return out, nil
}

// versionsByIdsUrl and projectsByIdsUrl serialize IDs as one JSON array in a
// query parameter, as the Modrinth API expects.
func versionsByIdsUrl(ids []string) string {
	return versionsByIdsUrlPrefix + url.QueryEscape(jsonArray(ids))
}

func projectsByIdsUrl(ids []string) string {
	return projectsByIdsUrlPrefix + url.QueryEscape(jsonArray(ids))
}

func jsonArray(values []string) string {
	encoded, _ := json.Marshal(values)
	return string(encoded)
}

// batchIds sorts and deduplicates ids in place. Every caller owns its slice.
func batchIds(ids []string) []string {
	slices.Sort(ids)
	return slices.Compact(ids)
}

func (p *Provider) requestBytes(
	ctx context.Context,
	rawURL string,
) (*cache.BytesResponse, error) {
	p.logger.Debug("modrinth api: GET " + rawURL)
	return p.network.CachedGetRequest(
		ctx,
		rawURL,
		cache.BytesRequestOptions{
			Kind:    cache.KindMetadata,
			Headers: metadataRequestHeaders,
		},
	)
}

func (p *Provider) requestJSON(
	ctx context.Context,
	rawURL string,
	out any,
	notFound error,
) error {
	res, err := p.requestBytes(ctx, rawURL)
	if err != nil {
		return fmt.Errorf("modrinth: request failed: %w", err)
	}
	if res.StatusCode == http.StatusNotFound && notFound != nil {
		return notFound
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf(
			"%w: status %d",
			ErrInvalidAPIResponse,
			res.StatusCode,
		)
	}
	if err := json.Unmarshal(res.Data, out); err != nil {
		if notFound != nil {
			return fmt.Errorf("%w: %w", notFound, err)
		}
		return fmt.Errorf("modrinth: failed to parse response: %w", err)
	}
	return nil
}
