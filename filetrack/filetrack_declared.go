package filetrack

import (
	"errors"
	"fmt"

	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fileglob"
)

// expandDeclaredPaths resolves patterns against tracked declarations without
// walking the workspace, so a declared file that no longer exists still
// matches. Recover reports matched presence-only declarations as skipped.
func expandDeclaredPaths(
	files []documentpkg.TrackedFile,
	paths []string,
	operation Operation,
) (selected, skipped []string, resultErr error) {
	type argument struct {
		raw  string
		path string
	}
	declared := make(map[string]documentpkg.TrackedFile, len(files))
	declaredPaths := make([]string, 0, len(files))
	for _, file := range files {
		declared[file.Path] = file
		declaredPaths = append(declaredPaths, file.Path)
	}
	arguments := make([]argument, 0, len(paths))
	literal := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		path, err := input.TrackedPath(raw)
		if err != nil {
			return nil, nil, err
		}
		arguments = append(arguments, argument{raw: raw, path: path})
		_, exists := declared[path]
		if !fileglob.IsPattern(path) || exists {
			literal[path] = struct{}{}
		}
	}
	selected = make([]string, 0, len(files))
	skipped = make([]string, 0)
	selectedSeen := make(map[string]struct{}, len(files))
	skippedSeen := make(map[string]struct{}, len(files))
	addSelected := func(path string) {
		if _, exists := selectedSeen[path]; exists {
			return
		}
		selectedSeen[path] = struct{}{}
		selected = append(selected, path)
	}
	addSkipped := func(path string) {
		if _, exists := skippedSeen[path]; exists {
			return
		}
		skippedSeen[path] = struct{}{}
		skipped = append(skipped, path)
	}
	for _, argument := range arguments {
		if _, exact := literal[argument.path]; exact {
			addSelected(argument.path)
			continue
		}
		matches, matchErr := fileglob.MatchPaths(argument.path, declaredPaths)
		if errors.Is(matchErr, fileglob.ErrInvalidPattern) {
			return nil, nil, fmt.Errorf("invalid pattern %q", argument.raw)
		}
		if matchErr != nil {
			return nil, nil, matchErr
		}
		matched := false
		for _, path := range matches {
			matched = true
			if _, explicit := literal[path]; explicit {
				continue
			}
			file, exists := declared[path]
			if !exists {
				continue
			}
			if operation == Recover && file.Hash == "" {
				addSkipped(path)
				continue
			}
			addSelected(path)
		}
		if !matched {
			return nil, nil, fmt.Errorf(
				"pattern %q matches no tracked files",
				argument.raw,
			)
		}
	}
	return selected, skipped, nil
}
