package github

import (
	"encoding/json/v2"
	"fmt"
	"net/http"

	"github.com/mclucy/lucy/cache"
)

// checkGitHubMessage checks if the response data is a GitHub API error message
// Returns the parsed message if it is an error message, nil otherwise
func checkGitHubMessage(data []byte) *GhApiMessage {
	var msg *GhApiMessage
	err := json.Unmarshal(data, &msg)
	if err == nil && msg != nil && msg.Message != "" {
		return msg
	}
	return nil
}

func GetFileFromGitHub(apiEndpoint string) (
	err error,
	msg *GhApiMessage,
	data []byte,
) {
	res, err := cachedGithubGet(apiEndpoint)
	if err != nil {
		return err, nil, nil
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("github: request failed: status %d", res.StatusCode), nil, nil
	}
	data = res.Data

	// Check if the response is an error message from GitHub API
	if msg := checkGitHubMessage(data); msg != nil {
		return nil, msg, data
	}

	var item GhItem
	err = json.Unmarshal(data, &item)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCannotDecode, err), nil, nil
	}
	res, err = cachedGithubGet(item.DownloadUrl)
	if err != nil {
		return err, nil, nil
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("github: download failed: status %d", res.StatusCode), nil, nil
	}
	data = res.Data

	return nil, nil, data
}

func GetDirectoryFromGitHub(apiEndpoint string) (
	err error,
	msg *GhApiMessage,
	items []GhItem,
) {
	res, err := cachedGithubGet(apiEndpoint)
	if err != nil {
		return err, nil, nil
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("github: request failed: status %d", res.StatusCode), nil, nil
	}
	data := res.Data

	// Check if the response is an error message from GitHub API
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

func cachedGithubGet(rawURL string) (*cache.BytesResponse, error) {
	return cache.CachedGetRequest(
		rawURL,
		cache.BytesRequestOptions{Kind: cache.KindMetadata},
	)
}
