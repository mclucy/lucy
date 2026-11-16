package filetrack

import (
	"errors"
	"fmt"
	"os"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fileglob"
)

// expandFilePaths resolves shell-style workspace patterns into the entries
// they select. Literal paths pass through normalization. A path in exact
// selects itself even when the workspace entry is gone, so a deleted
// declaration stays addressable by name. Directories are reported separately
// because a tracked entry is always a regular file. A pattern that matches
// neither a file nor a directory fails, so a typo cannot silently select
// nothing.
func expandFilePaths(root *os.Root, paths []string, exact map[string]struct{}) (selected, skippedDirectories []string, resultErr error) {
	selected = make([]string, 0, len(paths))
	skippedDirectories = make([]string, 0)
	seen := make(map[string]struct{}, len(paths))
	add := func(name string, directory bool) bool {
		normalized, err := input.TrackedPath(name)
		if err != nil {
			return false
		}
		if _, exists := seen[normalized]; exists {
			return true
		}
		seen[normalized] = struct{}{}
		if directory {
			skippedDirectories = append(skippedDirectories, normalized)
		} else {
			selected = append(selected, normalized)
		}
		return true
	}
	for _, raw := range paths {
		normalized, err := input.TrackedPath(raw)
		if err != nil {
			return nil, nil, err
		}
		if _, ok := exact[normalized]; ok {
			add(normalized, false)
			continue
		}
		if !fileglob.IsPattern(normalized) {
			info, statErr := root.Lstat(normalized)
			switch {
			case statErr == nil:
				add(normalized, info.IsDir())
			case errors.Is(statErr, os.ErrNotExist):
				add(normalized, false)
			default:
				return nil, nil, statErr
			}
			continue
		}
		if info, statErr := root.Lstat(normalized); statErr == nil {
			add(normalized, info.IsDir())
			continue
		}
		entries, matchErr := fileglob.Match(root.FS(), normalized)
		if errors.Is(matchErr, fileglob.ErrInvalidPattern) {
			return nil, nil, fmt.Errorf("invalid pattern %q", raw)
		}
		if matchErr != nil {
			return nil, nil, matchErr
		}
		matched := false
		for _, entry := range entries {
			if add(entry.Path, entry.Dir) {
				matched = true
			}
		}
		if !matched {
			return nil, nil, fmt.Errorf(
				"pattern %q matches no files or directories in this workspace",
				raw,
			)
		}
	}
	return selected, skippedDirectories, nil
}
