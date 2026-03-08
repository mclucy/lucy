package curseforge

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
)

const baseUrl = "https://api.curseforge.com"

// ApiKey is injected at build time via ldflags:
//
//	go build -ldflags "-X github.com/mclucy/lucy/upstream/curseforge.ApiKey=YOUR_KEY"
var ApiKey string

// get performs an authenticated GET request to the CurseForge API and
// unmarshals the JSON response into dest.
func get(url string, dest any) error {
	if ApiKey == "" {
		return ErrNoApiKey
	}

	logger.Debug("curseforge api: GET " + url)

	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("curseforge: failed to create request: %w", err)
	}
	req.Header.Set("x-api-key", ApiKey)
	req.Header.Set("Accept", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("curseforge: request failed: %w", err)
	}
	defer tools.CloseReader(res.Body, logger.Warn)

	if res.StatusCode != http.StatusOK {
		return ErrApiResponse(res.StatusCode)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("curseforge: failed to read response: %w", err)
	}

	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("curseforge: failed to parse response: %w", err)
	}

	return nil
}
