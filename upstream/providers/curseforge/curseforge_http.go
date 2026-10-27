package curseforge

import (
	"encoding/json/v2"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/internal/cipher"
	"github.com/mclucy/lucy/log"
)

// Docs: https://docs.curseforge.com/rest-api/
const baseUrl = "https://api.curseforge.com"

var (
	ApiKey     string
	userAPIKey string

	loadApiKey = sync.OnceValue(func() error {
		key, err := cipher.Decode()
		if err != nil {
			return fmt.Errorf("curseforge: decode embedded api key: %w", err)
		}
		ApiKey = strings.TrimSpace(key)
		return nil
	})
)

// SetUserAPIKey configures the key from machine configuration. An empty key
// keeps the key embedded in the build.
func SetUserAPIKey(key string) {
	userAPIKey = strings.TrimSpace(key)
}

func effectiveAPIKey() (string, error) {
	if userAPIKey != "" {
		return userAPIKey, nil
	}
	if err := loadApiKey(); err != nil {
		return "", err
	}
	return ApiKey, nil
}

// get performs an authenticated GET request to the CurseForge API and
// unmarshals the JSON response into dest.
func get(url string, dest any) error {
	key, err := effectiveAPIKey()
	if err != nil {
		return err
	}
	if key == "" {
		return ErrNoApiKey
	}

	log.Debug("curseforge api: GET " + url)

	headers := http.Header{}
	headers.Set("x-api-key", key)
	headers.Set("Accept", "application/json")
	res, err := cache.CachedGetRequest(
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
