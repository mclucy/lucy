package github

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/log"
)

// Client fetches GitHub metadata through one configured network cache.
type Client struct {
	network *cache.Client
	logger  *log.Logger
}

// New constructs a GitHub metadata client. network supplies the cached HTTP
// transport; logger may be nil, which records nothing.
func New(network *cache.Client, logger *log.Logger) *Client {
	return &Client{network: network, logger: logger}
}

// checkGitHubMessage returns the parsed error message when data is a GitHub
// API error envelope, nil otherwise.
func checkGitHubMessage(data []byte) *GhApiMessage {
	var msg *GhApiMessage
	err := json.Unmarshal(data, &msg)
	if err == nil && msg != nil && msg.Message != "" {
		return msg
	}
	return nil
}

func (c *Client) GetFileFromGitHub(
	ctx context.Context,
	apiEndpoint string,
) (
	err error,
	msg *GhApiMessage,
	data []byte,
) {
	res, err := c.get(ctx, apiEndpoint)
	if err != nil {
		return err, nil, nil
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("github: request failed: status %d", res.StatusCode), nil, nil
	}
	data = res.Data

	if msg := checkGitHubMessage(data); msg != nil {
		return nil, msg, data
	}

	var item GhItem
	err = json.Unmarshal(data, &item)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCannotDecode, err), nil, nil
	}
	res, err = c.get(ctx, item.DownloadUrl)
	if err != nil {
		return err, nil, nil
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("github: download failed: status %d", res.StatusCode), nil, nil
	}
	data = res.Data

	return nil, nil, data
}

func (c *Client) GetDirectoryFromGitHub(
	ctx context.Context,
	apiEndpoint string,
) (
	err error,
	msg *GhApiMessage,
	items []GhItem,
) {
	res, err := c.get(ctx, apiEndpoint)
	if err != nil {
		return err, nil, nil
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("github: request failed: status %d", res.StatusCode), nil, nil
	}
	data := res.Data

	if msg := checkGitHubMessage(data); msg != nil {
		return nil, msg, nil
	}

	var decodedItems []GhItem
	err = json.Unmarshal(data, &decodedItems)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCannotDecode, err), nil, nil
	}
	return nil, nil, decodedItems
}

func (c *Client) get(
	ctx context.Context,
	rawURL string,
) (*cache.BytesResponse, error) {
	c.logger.Debug("github api: GET " + rawURL)
	return c.network.CachedGetRequest(
		ctx,
		rawURL,
		cache.BytesRequestOptions{Kind: cache.KindMetadata},
	)
}
