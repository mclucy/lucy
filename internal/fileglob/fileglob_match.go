// Package fileglob selects filesystem paths with shell-style patterns.
package fileglob

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/bmatcuk/doublestar/v4"
)

// Entry is a path that a pattern selects.
type Entry struct {
	// Path is relative to the filesystem root and uses forward slashes.
	Path string
	// Dir reports whether Path is a directory.
	Dir bool
}

// ErrInvalidPattern marks a malformed pattern.
var ErrInvalidPattern = errors.New("invalid pattern")

// Match returns the entries below fsys that pattern selects, in walk order.
// A pattern that selects nothing returns an empty result and no error.
//
// The supported syntax is *, ?, character classes, and **. A directory is
// returned when the pattern selects it. A hidden path component needs an
// explicit dot in the matching pattern segment. The .lucy and .git
// directories are never returned.
func Match(fsys fs.FS, pattern string) ([]Entry, error) {
	matcher := globPattern(pattern)
	pruned := &prunedFS{FS: fsys, dots: newDotPolicy(matcher)}
	entries := make([]Entry, 0)
	err := doublestar.GlobWalk(
		pruned,
		matcher,
		func(name string, entry fs.DirEntry) error {
			if reservedPath(name) || !explicitlySelectsHidden(matcher, name) {
				return nil
			}
			switch {
			case entry.IsDir():
				entries = append(entries, Entry{Path: name, Dir: true})
			case entry.Type().IsRegular():
				entries = append(entries, Entry{Path: name})
			}
			return nil
		},
		doublestar.WithFailOnIOErrors(),
		doublestar.WithNoFollow(),
		doublestar.WithNoHidden(),
	)
	if errors.Is(err, doublestar.ErrBadPattern) {
		return nil, fmt.Errorf("%w %q", ErrInvalidPattern, pattern)
	}
	if err != nil {
		return nil, err
	}
	return entries, nil
}

// MatchPaths returns the paths that pattern selects, in input order. MatchPaths
// does not read a filesystem, so a declared path that no longer exists still
// matches. The hidden and syntax rules are the same as Match.
func MatchPaths(pattern string, paths []string) ([]string, error) {
	matcher := globPattern(pattern)
	if !doublestar.ValidatePattern(matcher) {
		return nil, fmt.Errorf("%w %q", ErrInvalidPattern, pattern)
	}
	matched := make([]string, 0, len(paths))
	for _, path := range paths {
		if reservedPath(path) || !explicitlySelectsHidden(matcher, path) {
			continue
		}
		matched = append(matched, path)
	}
	return matched, nil
}
