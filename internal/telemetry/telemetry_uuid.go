package telemetry

import (
	"fmt"
	"strings"
	"uuid"
)

// NewUUID returns a random RFC 9562 version 4 UUID from the standard library.
func NewUUID() string {
	return uuid.NewV4().String()
}

func normalizeUUID(raw string) (string, error) {
	parsed, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("parse uuid: %w", err)
	}
	return parsed.String(), nil
}
