// Package fileglob selects filesystem paths with shell-style patterns.
package fileglob

import (
	"errors"
	"fmt"
	"io/fs"
	"strings"

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
	entries := make([]Entry, 0)
	err := doublestar.GlobWalk(
		fsys,
		matcher,
		func(name string, entry fs.DirEntry) error {
			if !explicitlySelectsHidden(matcher, name) {
				return nil
			}
			switch {
			case entry.IsDir():
				if reservedDirectory(entry) {
					return doublestar.SkipDir
				}
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

// reservedDirectory reports whether the directory holds Lucy or version
// control state. A pattern never selects it.
func reservedDirectory(entry fs.DirEntry) bool {
	switch strings.ToLower(entry.Name()) {
	case ".lucy", ".git":
		return true
	default:
		return false
	}
}
