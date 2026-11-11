package filetrack

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

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
	var resolver *patternResolver
	for _, raw := range paths {
		normalized, err := input.TrackedPath(raw)
		if err != nil {
			return nil, nil, err
		}
		if !strings.ContainsAny(normalized, "*?[") {
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
		if err := validateFilePattern(normalized); err != nil {
			return nil, nil, err
		}
		if resolver == nil {
			resolver = &patternResolver{
				fsys:    root.FS(),
				entries: make(map[string][]fs.DirEntry),
			}
		}
		matched := false
		err = resolver.match(
			".",
			strings.Split(normalized, "/"),
			func(name string, directory bool) bool {
				if !add(name, directory) {
					return false
				}
				matched = true
				return true
			},
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

// validateFilePattern rejects malformed character classes before traversal can
// report them as a match failure.
func validateFilePattern(pattern string) error {
	for segment := range strings.SplitSeq(pattern, "/") {
		if segment == "**" {
			continue
		}
		if _, err := path.Match(negatedTrackClass(segment), ""); err != nil {
			return fmt.Errorf("invalid pattern %q: %w", pattern, err)
		}
	}
	return nil
}

// patternResolver descends only through directories a pattern can select, so
// an unrelated unreadable directory cannot prevent expansion.
type patternResolver struct {
	fsys    fs.FS
	entries map[string][]fs.DirEntry
}

func (r *patternResolver) readDir(name string) ([]fs.DirEntry, error) {
	if entries, exists := r.entries[name]; exists {
		return entries, nil
	}
	entries, err := fs.ReadDir(r.fsys, name)
	if err != nil {
		return nil, fmt.Errorf("read workspace directory %s: %w", name, err)
	}
	r.entries[name] = entries
	return entries, nil
}

// match emits every entry below directory that segments select. The caller
// filters emitted paths through the tracked-path contract.
func (r *patternResolver) match(
	directory string,
	segments []string,
	emit func(name string, directory bool) bool,
) error {
	if len(segments) == 0 {
		if directory != "." {
			emit(directory, true)
		}
		return nil
	}
	entries, err := r.readDir(directory)
	if err != nil {
		return err
	}
	if segments[0] == "**" {
		if err := r.match(directory, segments[1:], emit); err != nil {
			return err
		}
		for _, entry := range entries {
			name := entry.Name()
			if strings.HasPrefix(name, ".") || reservedDirectory(entry) {
				continue
			}
			child := path.Join(directory, name)
			switch {
			case entry.IsDir():
				if err := r.match(child, segments, emit); err != nil {
					return err
				}
			case entry.Type().IsRegular() && len(segments) == 1:
				emit(child, false)
			}
		}
		return nil
	}
	segment := segments[0]
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && !explicitDotSegment(segment) {
			continue
		}
		if reservedDirectory(entry) {
			continue
		}
		matched, err := path.Match(negatedTrackClass(segment), name)
		if err != nil {
			return err
		}
		if !matched {
			continue
		}
		child := path.Join(directory, name)
		if len(segments) == 1 {
			switch {
			case entry.IsDir():
				emit(child, true)
			case entry.Type().IsRegular():
				emit(child, false)
			}
			continue
		}
		if entry.IsDir() {
			if err := r.match(child, segments[1:], emit); err != nil {
				return err
			}
		}
	}
	return nil
}

func reservedDirectory(entry fs.DirEntry) bool {
	if !entry.IsDir() {
		return false
	}
	switch strings.ToLower(entry.Name()) {
	case ".lucy", ".git":
		return true
	default:
		return false
	}
}

// explicitDotSegment reports whether a segment explicitly selects a leading
// dot. Negated classes and wildcards do not.
func explicitDotSegment(segment string) bool {
	if strings.HasPrefix(segment, ".") {
		return true
	}
	if !strings.HasPrefix(segment, "[") {
		return false
	}
	end := strings.IndexByte(segment, ']')
	if end < 0 {
		return false
	}
	class := segment[1:end]
	if class == "" || class[0] == '!' || class[0] == '^' {
		return false
	}
	for index := range class {
		if class[index] == '.' {
			return true
		}
		if index+2 < len(class) &&
			class[index+1] == '-' &&
			class[index] <= '.' &&
			'.' <= class[index+2] {
			return true
		}
	}
	return false
}

// negatedTrackClass accepts the shell spelling [!abc] alongside Go's [^abc].
func negatedTrackClass(segment string) string {
	if !strings.Contains(segment, "[!") {
		return segment
	}
	return strings.ReplaceAll(segment, "[!", "[^")
}
