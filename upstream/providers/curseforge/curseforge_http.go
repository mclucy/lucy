package curseforge

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"

	"github.com/mclucy/lucy/cache"
)

// Docs: https://docs.curseforge.com/rest-api/
const baseUrl = "https://api.curseforge.com"

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
