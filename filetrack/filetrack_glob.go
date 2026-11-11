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

// expandTrackPaths resolves shell-style workspace patterns into the entries
// they select. Literal paths pass through normalization. Directories are
// reported as skipped because a tracked entry is always a regular file. A
// pattern that matches neither a file nor a directory fails, so a typo cannot
// silently track nothing.
func expandTrackPaths(root *os.Root, paths []string) (selected, skipped []string, resultErr error) {
	selected = make([]string, 0, len(paths))
	skipped = make([]string, 0)
	seen := make(map[string]struct{}, len(paths))
	appendPath := func(destination *[]string, value string) {
		if _, exists := seen[value]; exists {
			return
		}
		seen[value] = struct{}{}
		*destination = append(*destination, value)
	}
	var files, directories []string
	loaded := false
	for _, raw := range paths {
		normalized, err := input.TrackedPath(raw)
		if err != nil {
			return nil, nil, err
		}
		if !strings.ContainsAny(normalized, "*?[") {
			info, err := root.Lstat(normalized)
			switch {
			case errors.Is(err, os.ErrNotExist):
				appendPath(&selected, normalized)
			case err != nil:
				return nil, nil, err
			case info.IsDir():
				appendPath(&skipped, normalized)
			default:
				appendPath(&selected, normalized)
			}
			continue
		}
		if err := validateTrackPattern(normalized); err != nil {
			return nil, nil, err
		}
		if !loaded {
			files, directories, err = trackableEntries(root)
			if err != nil {
				return nil, nil, err
			}
			loaded = true
		}
		matched := false
		for _, candidate := range files {
			if !matchTrackPattern(normalized, candidate) {
				continue
			}
			appendPath(&selected, candidate)
			matched = true
		}
		for _, candidate := range directories {
			if !matchTrackPattern(normalized, candidate) {
				continue
			}
			appendPath(&skipped, candidate)
			matched = true
		}
		if !matched {
			return nil, nil, fmt.Errorf(
				"track pattern %q matches no files or directories in this workspace",
				raw,
			)
		}
	}
	return selected, skipped, nil
}

// validateTrackPattern rejects malformed character classes before a walk can
// report them as a match failure.
func validateTrackPattern(pattern string) error {
	for segment := range strings.SplitSeq(pattern, "/") {
		if segment == "**" {
			continue
		}
		if _, err := path.Match(negatedTrackClass(segment), ""); err != nil {
			return fmt.Errorf("invalid track pattern %q: %w", pattern, err)
		}
	}
	return nil
}

// trackableEntries lists every regular file and directory that a track
// pattern may select. Reserved state directories are never candidates, and
// paths that the manifest cannot declare are skipped without failing the
// whole walk.
func trackableEntries(root *os.Root) (files, directories []string, resultErr error) {
	err := fs.WalkDir(
		root.FS(),
		".",
		func(name string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if name == "." {
				return nil
			}
			if entry.IsDir() {
				switch strings.ToLower(entry.Name()) {
				case ".lucy", ".git":
					return fs.SkipDir
				}
				normalized, err := input.TrackedPath(name)
				if err != nil {
					return nil
				}
				directories = append(directories, normalized)
				return nil
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			normalized, err := input.TrackedPath(name)
			if err != nil {
				return nil
			}
			files = append(files, normalized)
			return nil
		},
	)
	if err != nil {
		return nil, nil, err
	}
	return files, directories, nil
}

// matchTrackPattern reports whether a workspace-relative path matches a
// pattern. Wildcards match within one component; ** matches zero or more
// components. A leading dot is matched only by an explicit dot or character
// class, as in a shell.
func matchTrackPattern(pattern, name string) bool {
	return matchTrackSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

func matchTrackSegments(pattern, name []string) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}
	if pattern[0] == "**" {
		if matchTrackSegments(pattern[1:], name) {
			return true
		}
		if len(name) == 0 || strings.HasPrefix(name[0], ".") {
			return false
		}
		return matchTrackSegments(pattern, name[1:])
	}
	if len(name) == 0 {
		return false
	}
	if strings.HasPrefix(name[0], ".") && !explicitDotSegment(pattern[0]) {
		return false
	}
	matched, err := path.Match(negatedTrackClass(pattern[0]), name[0])
	return err == nil && matched && matchTrackSegments(pattern[1:], name[1:])
}

func explicitDotSegment(segment string) bool {
	return strings.HasPrefix(segment, ".") || strings.HasPrefix(segment, "[")
}

// negatedTrackClass accepts the shell spelling [!abc] alongside Go's [^abc].
func negatedTrackClass(segment string) string {
	if !strings.Contains(segment, "[!") {
		return segment
	}
	return strings.ReplaceAll(segment, "[!", "[^")
}
