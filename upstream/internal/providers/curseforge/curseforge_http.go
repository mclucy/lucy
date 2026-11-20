package curseforge

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/internal/fsutil"
)

// Docs: https://docs.curseforge.com/rest-api/
const baseUrl = "https://api.curseforge.com"

// maxLookupResponseBytes bounds one batched lookup response. CurseForge
// documents no cap on request arrays; lookupBatchSize bounds them here, so a
// response stays bounded even when a project carries many files.
const maxLookupResponseBytes = 16 << 20

// get performs an authenticated GET request to the CurseForge API and
// unmarshals the JSON response into dest.
func (p *Provider) get(ctx context.Context, url string, dest any) error {
	if p.apiKey == "" {
		return ErrNoApiKey
	}

	p.logger.Debug("curseforge api: GET " + url)

	headers := http.Header{}
	headers.Set("x-api-key", p.apiKey)
	headers.Set("Accept", "application/json")
	res, err := p.network.CachedGetRequest(
		ctx,
		url,
		cache.BytesRequestOptions{
			Kind:    cache.KindMetadata,
			Headers: headers,
		},
	)
	if err != nil {
		return fmt.Errorf("curseforge: request failed: %w", err)
	}

	if res.StatusCode != http.StatusOK {
		return ErrApiResponse(res.StatusCode)
	}

	if err := json.Unmarshal(res.Data, dest); err != nil {
		return fmt.Errorf("curseforge: failed to parse response: %w", err)
	}

	return nil
}

// post performs an authenticated JSON POST to the CurseForge API and
// unmarshals a 200 response into dest. Non-200 statuses are returned as
// ApiResponseError so callers can tell a missing resource (404) from a failed
// lookup. POST lookups are not cached: the cache keys responses by URL, while
// these responses depend on the request body.
func (p *Provider) post(
	ctx context.Context,
	url string,
	payload, dest any,
) error {
	if p.apiKey == "" {
		return ErrNoApiKey
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("curseforge: failed to encode request: %w", err)
	}

	p.logger.Debug("curseforge api: POST " + url)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		url,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("curseforge: request failed: %w", err)
	}
	req.Header.Set("x-api-key", p.apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	//nolint:bodyclose // CloseReader closes the response and logs close errors.
	resp, err := p.network.Do(req)
	if err != nil {
		return fmt.Errorf("curseforge: request failed: %w", err)
	}
	defer fn.CloseReader(resp.Body, p.logger.Warn)

	if resp.StatusCode != http.StatusOK {
		return ErrApiResponse(resp.StatusCode)
	}

	raw, err := fsutil.CopyBytes(resp.Body, maxLookupResponseBytes)
	if err != nil {
		return fmt.Errorf("curseforge: failed to read response: %w", err)
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("curseforge: failed to parse response: %w", err)
	}

	return nil
}

// isNotFound reports whether err is an API 404 response.
func isNotFound(err error) bool {
	var apiErr ApiResponseError
	return errors.As(err, &apiErr) &&
		apiErr.StatusCode == http.StatusNotFound
}
