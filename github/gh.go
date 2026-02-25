package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
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
	resp, err := http.Get(apiEndpoint)
	if err != nil {
		return err, nil, nil
	}
	defer tools.CloseReader(resp.Body, logger.Warn)
	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return err, nil, nil
	}

	// Check if the response is an error message from GitHub API
	if msg := checkGitHubMessage(data); msg != nil {
		return nil, msg, data
	}

	var item GhItem
	err = json.Unmarshal(data, &item)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCannotDecode, err), nil, nil
	}
	resp, err = http.Get(item.DownloadUrl)
	if err != nil {
		return err, nil, nil
	}
	defer tools.CloseReader(resp.Body, logger.Warn)
	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return err, nil, nil
	}

	return nil, nil, data
}

func GetDirectoryFromGitHub(apiEndpoint string) (
	err error,
	msg *GhApiMessage,
	items []GhItem,
) {
	resp, err := http.Get(apiEndpoint)
	if err != nil {
		return err, nil, nil
	}
	defer tools.CloseReader(resp.Body, logger.Warn)
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err, nil, nil
	}

	// Check if the response is an error message from GitHub API
	if msg := checkGitHubMessage(data); msg != nil {
		return nil, msg, nil
	}

	var res []GhItem
	err = json.Unmarshal(data, &res)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrCannotDecode, err), nil, nil
	}
	return nil, nil, res
}
