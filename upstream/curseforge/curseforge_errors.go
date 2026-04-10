package curseforge

import (
	"errors"
	"fmt"
)

type ApiResponseError struct {
	StatusCode int
}

func (e ApiResponseError) Error() string {
	return fmt.Sprintf("curseforge: API returned status %d", e.StatusCode)
}

var (
	ErrProjectNotFound    = errors.New("curseforge: project not found")
	ErrAmbiguousSlug      = errors.New("curseforge: ambiguous slug, multiple projects matched")
	ErrDownloadNotAllowed = errors.New("curseforge: download not allowed by mod author")
	ErrNoCompatibleFile   = errors.New("curseforge: no compatible file found")
	ErrNoApiKey           = errors.New("curseforge: API key not configured")
	ErrInvalidApiKey      = errors.New("curseforge: API key rejected")
	ErrApiResponse        = func(statusCode int) error {
		return ApiResponseError{StatusCode: statusCode}
	}
)
