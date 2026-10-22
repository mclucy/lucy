package input

import (
	"cmp"
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/types"
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
		strings.EqualFold(first, "lucy-lock.yaml") ||
		strings.EqualFold(first, "lucy.local.yaml") {
		return "", fmt.Errorf("tracked path names Lucy state or a directory: %q", value)
	}
	return value, nil
}

// NormalizeTrackedFiles makes declaration order irrelevant to manifest fingerprints.
func NormalizeTrackedFiles(files []types.TrackedFile) error {
	seen := make(map[string]struct{}, len(files))
	for i := range files {
		file := &files[i]
		normalized, err := TrackedPath(file.Path)
		if err != nil {
			return err
		}
		file.Path = normalized
		key := strings.ToLower(normalized)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate tracked path %q", normalized)
		}
		seen[key] = struct{}{}
		if file.Immutable && file.Hash == "" {
			return fmt.Errorf("immutable file %s requires an accepted hash", file.Path)
		}
		if file.Hash != "" {
			if len(file.Hash) != 71 || !strings.HasPrefix(file.Hash, "sha256:") {
				return fmt.Errorf("tracked file %s requires sha256:<64 lowercase hexadecimal characters>", file.Path)
			}
			for _, digit := range file.Hash[7:] {
				if !((digit >= '0' && digit <= '9') || (digit >= 'a' && digit <= 'f')) {
					return fmt.Errorf("invalid accepted hash for %s", file.Path)
				}
			}
		}
		if file.RecoverURL != "" {
			if file.Hash == "" {
				return fmt.Errorf("recovery URL for %s requires an accepted hash", file.Path)
			}
			u, err := url.Parse(file.RecoverURL)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
				return fmt.Errorf("invalid recovery URL for %s", file.Path)
			}
		}
	}
	slices.SortFunc(files, func(a, b types.TrackedFile) int { return cmp.Compare(a.Path, b.Path) })
	return nil
}
