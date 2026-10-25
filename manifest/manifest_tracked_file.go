package manifest

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/mclucy/lucy/input"
)

// TrackedFile declares accepted contents and a change policy, not an observed file state.
type TrackedFile struct {
	Path       string `yaml:"path" json:"path"`
	Immutable  bool   `yaml:"immutable,omitempty" json:"immutable,omitzero"`
	Hash       string `yaml:"hash,omitempty" json:"hash,omitempty"`
	RecoverURL string `yaml:"recover_url,omitempty" json:"recover_url,omitempty"`
}

// NormalizeTrackedFiles makes declaration order irrelevant to manifest fingerprints.
func NormalizeTrackedFiles(files []TrackedFile) error {
	seen := make(map[string]struct{}, len(files))
	for i := range files {
		file := &files[i]
		normalized, err := input.TrackedPath(file.Path)
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
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return nil
}
