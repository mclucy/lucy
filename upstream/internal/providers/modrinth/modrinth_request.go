package modrinth

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/url"
	"slices"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/upstream/internal/providers/retry"
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
		return nil, retry.Permanent(
			fmt.Errorf("modrinth: encode hash lookup: %w", err),
		)
	}

	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("Accept", "application/json")
	headers.Set("User-Agent", modrinthUserAgent)

	p.logger.Debug("modrinth api: POST " + versionFilesUrl)

	var out map[string]versionResponse
	outcome := retry.Do(
		ctx,
		retry.Default,
		func(ctx context.Context) retry.Attempt {
			res, err := p.network.CachedPostRequest(
				ctx,
				versionFilesUrl,
				body,
				cache.BytesRequestOptions{
					Kind:     cache.KindMetadata,
					Headers:  headers,
					MaxBytes: maxBatchResponseBytes,
				},
				func(raw []byte) error {
					if err := json.Unmarshal(raw, &out); err != nil {
						return retry.Permanent(fmt.Errorf("modrinth: failed to parse response: %w", err))
					}
					return nil
				},
			)
			if err != nil {
				return retry.Attempt{Err: err}
			}
			if res.StatusCode != http.StatusOK {
				p.logger.Debug(
					fmt.Sprintf(
						"modrinth api: POST %s returned status %d",
						versionFilesUrl,
						res.StatusCode,
					),
				)
				return retry.Attempt{
					Status:     res.StatusCode,
					RetryAfter: retry.RetryAfter(res.Header),
				}
			}
			return retry.Attempt{Status: res.StatusCode}
		},
	)
	if outcome.Err != nil {
		return nil, fmt.Errorf("modrinth: request failed: %w", outcome.Err)
	}
	if outcome.Status != http.StatusOK {
		return nil, fmt.Errorf(
			"%w: %w",
			ErrInvalidAPIResponse,
			&retry.StatusError{Status: outcome.Status},
		)
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
