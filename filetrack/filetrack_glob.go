package filetrack

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/input"
)

// expandFilePaths resolves shell-style workspace patterns into the entries
// they select. Literal paths pass through normalization. Directories are
// reported separately because a tracked entry is always a regular file. A
// pattern that matches neither a file nor a directory fails, so a typo cannot
// silently select nothing.
func expandFilePaths(root *os.Root, paths []string) (selected, skippedDirectories []string, resultErr error) {
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
		if !hasPatternMeta(normalized) {
			info, err := root.Lstat(normalized)
			switch {
			case errors.Is(err, os.ErrNotExist):
				add(normalized, false)
			case err != nil:
				return nil, nil, err
			case info.IsDir():
				add(normalized, true)
			default:
				add(normalized, false)
			}
			continue
		}
		pattern := globPattern(normalized)
		if !doublestar.ValidatePattern(pattern) {
			return nil, nil, fmt.Errorf("invalid pattern %q", raw)
		}
		matched := false
		err = doublestar.GlobWalk(
			root.FS(),
			pattern,
			func(name string, entry fs.DirEntry) error {
				if entry.IsDir() && reservedDirectory(entry) {
					return doublestar.SkipDir
				}
				switch {
				case entry.IsDir():
					if add(name, true) {
						matched = true
					}
				case entry.Type().IsRegular():
					if add(name, false) {
						matched = true
					}
				}
				return nil
			},
			doublestar.WithFailOnIOErrors(),
			doublestar.WithNoFollow(),
			doublestar.WithNoHidden(),
		)
		if err != nil {
			return nil, nil, err
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

// globPattern adapts the supported pattern to doublestar. Braces stay literal
// because only *, ?, character classes, and ** are documented. A leading
// negated class would otherwise match a leading dot, which WithNoHidden does
// not suppress.
func globPattern(pattern string) string {
	pattern = strings.NewReplacer(
		"{", `\{`,
		"}", `\}`,
	).Replace(pattern)
	segments := strings.Split(pattern, "/")
	for index, segment := range segments {
		if !strings.HasPrefix(segment, "[!") && !strings.HasPrefix(segment, "[^") {
			continue
		}
		end := strings.IndexByte(segment, ']')
		if end < 0 {
			continue
		}
		content := segment[1:end]
		insert := end
		if strings.HasSuffix(content, "-") {
			insert--
		}
		segments[index] = segment[:insert] + "." + segment[insert:]
	}
	return strings.Join(segments, "/")
}

func reservedDirectory(entry fs.DirEntry) bool {
	switch strings.ToLower(entry.Name()) {
	case ".lucy", ".git":
		return true
	default:
		return false
	}
}

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
	arguments := make([]argument, 0, len(paths))
	literal := make(map[string]struct{}, len(paths))
	for _, raw := range paths {
		path, err := input.TrackedPath(raw)
		if err != nil {
			return nil, nil, err
		}
		arguments = append(arguments, argument{raw: raw, path: path})
		if !hasPatternMeta(path) {
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
		if !hasPatternMeta(argument.path) {
			addSelected(argument.path)
			continue
		}
		pattern := globPattern(argument.path)
		if !doublestar.ValidatePattern(pattern) {
			return nil, nil, fmt.Errorf("invalid pattern %q", argument.raw)
		}
		matched := false
		for _, file := range files {
			ok, err := doublestar.Match(pattern, file.Path)
			if err != nil {
				return nil, nil, err
			}
			if !ok {
				continue
			}
			matched = true
			if _, explicit := literal[file.Path]; explicit {
				continue
			}
			if operation == Recover && file.Hash == "" {
				addSkipped(file.Path)
				continue
			}
			addSelected(file.Path)
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

func hasPatternMeta(value string) bool {
	return strings.ContainsAny(value, "*?[")
}
