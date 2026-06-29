package modrinth

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/log"
)

func requestBytes(rawURL string) (*cache.BytesResponse, error) {
	log.Debug("modrinth api: GET " + rawURL)
	return cache.CachedGetRequest(
		rawURL,
		cache.BytesRequestOptions{Kind: cache.KindMetadata},
	)
}

func requestJSON(rawURL string, out any, notFound error) error {
	res, err := requestBytes(rawURL)
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
