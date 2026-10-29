package input

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// TrackedPath rejects paths that cannot safely name portable workspace files.
func TrackedPath(value string) (string, error) {
	if value == "" || strings.ContainsAny(value, "\\:\x00") || !filepath.IsLocal(value) {
		return "", fmt.Errorf("tracked path must be workspace-relative: %q", value)
	}
	for component := range strings.SplitSeq(value, "/") {
		if component == ".." || strings.EqualFold(component, ".lucy") || strings.EqualFold(component, ".git") {
			return "", fmt.Errorf("tracked path traverses reserved state: %q", value)
		}
	}
	value = path.Clean(value)
	first, _, _ := strings.Cut(value, "/")
	if value == "." ||
		strings.EqualFold(first, "lucy.yaml") ||
		strings.EqualFold(first, "lucy-lock.yaml") {
		return "", fmt.Errorf("tracked path names Lucy state or a directory: %q", value)
	}
	return value, nil
}
