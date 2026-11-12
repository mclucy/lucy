package filetrack

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
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
				if !explicitlySelectsHidden(pattern, name) {
					return nil
				}
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
// because only *, ?, character classes, and ** are documented.
func globPattern(pattern string) string {
	return strings.NewReplacer(
		"{", `\{`,
		"}", `\}`,
	).Replace(pattern)
}

// explicitlySelectsHidden reports whether pattern selects every hidden
// component of name with an explicit leading dot. Doublestar's WithNoHidden
// checks file attributes on Windows and cannot see in-memory declarations, so
// the documented leading-dot rule is enforced here instead.
func explicitlySelectsHidden(pattern, name string) bool {
	segments := strings.Split(pattern, "/")
	components := strings.Split(name, "/")
	type position struct{ segment, component int }
	type outcome struct {
		selects bool
		known   bool
	}
	memo := make(map[position]outcome, len(segments)*len(components))
	var search func(segment, component int) bool
	search = func(segment, component int) bool {
		at := position{segment, component}
		if result := memo[at]; result.known {
			return result.selects
		}
		selects := false
		switch {
		case segment == len(segments):
			selects = component == len(components)
		case segments[segment] == "**":
			selects = search(segment+1, component) ||
				component < len(components) &&
					!hiddenComponent(components[component]) &&
					search(segment, component+1)
		case component < len(components):
			matched, err := doublestar.Match(segments[segment], components[component])
			selects = err == nil && matched &&
				(!hiddenComponent(components[component]) ||
					explicitDotSegment(segments[segment])) &&
				search(segment+1, component+1)
		}
		memo[at] = outcome{selects: selects, known: true}
		return selects
	}
	return search(0, 0)
}

// hiddenComponent reports whether a path component is hidden to shell-style
// patterns.
func hiddenComponent(component string) bool {
	return strings.HasPrefix(component, ".")
}

// explicitDotSegment reports whether a pattern segment explicitly selects a
// leading dot through a literal dot, an escaped dot, or a non-negated
// character class that includes one.
func explicitDotSegment(segment string) bool {
	if strings.HasPrefix(segment, ".") {
		return true
	}
	if strings.HasPrefix(segment, `\`) {
		return len(segment) > 1 && segment[1] == '.'
	}
	class := leadingCharacterClass(segment)
	if class == "" {
		return false
	}
	content := class[1 : len(class)-1]
	if strings.HasPrefix(content, "!") || strings.HasPrefix(content, "^") {
		return false
	}
	matched, err := doublestar.Match(class, ".")
	return err == nil && matched
}

// leadingCharacterClass returns the first character class of a pattern
// segment, including its brackets, or an empty string when the segment does
// not start with one.
func leadingCharacterClass(segment string) string {
	if !strings.HasPrefix(segment, "[") {
		return ""
	}
	index := 1
	if index < len(segment) && (segment[index] == '!' || segment[index] == '^') {
		index++
	}
	if index < len(segment) && segment[index] == ']' {
		index++
	}
	for ; index < len(segment); index++ {
		switch segment[index] {
		case '\\':
			index++
		case ']':
			return segment[:index+1]
		}
	}
	return ""
}

// reservedDirectory reports whether the directory holds Lucy or version
// control state rather than server files. Patterns never select these.
func reservedDirectory(entry fs.DirEntry) bool {
	switch strings.ToLower(entry.Name()) {
	case ".lucy", ".git":
		return true
	default:
		return false
	}
}

func hasPatternMeta(value string) bool {
	return strings.ContainsAny(value, "*?[")
}
