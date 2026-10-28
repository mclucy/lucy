package modrinth

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"

	"github.com/mclucy/lucy/cache"
)

func (p *Provider) requestBytes(
	ctx context.Context,
	rawURL string,
) (*cache.BytesResponse, error) {
	p.logger.Debug("modrinth api: GET " + rawURL)
	return p.network.CachedGetRequest(
		ctx,
		rawURL,
		cache.BytesRequestOptions{Kind: cache.KindMetadata},
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
