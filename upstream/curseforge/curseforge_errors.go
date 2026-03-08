package curseforge

import (
	"errors"
	"fmt"
)

var (
	ErrProjectNotFound    = errors.New("curseforge: project not found")
	ErrAmbiguousSlug      = errors.New("curseforge: ambiguous slug, multiple projects matched")
	ErrDownloadNotAllowed = errors.New("curseforge: download not allowed by mod author")
	ErrNoCompatibleFile   = errors.New("curseforge: no compatible file found")
	ErrNoApiKey           = errors.New("curseforge: API key not configured")
	ErrApiResponse        = func(statusCode int) error {
		return fmt.Errorf("curseforge: API returned status %d", statusCode)
	}
)
