package fileglob

import (
	"errors"
	"io"
	"io/fs"
	"strings"
)

// prunedFS hides entries that the pattern cannot select before doublestar
// reads the directory. A callback never sees an excluded subtree, so hidden
// and reserved directories stay out of the walk even when they are
// intermediate path segments.
type prunedFS struct {
	fs.FS
	dots dotPolicy
}

func (p *prunedFS) Open(name string) (fs.File, error) {
	file, err := p.FS.Open(name)
	if err != nil {
		return nil, err
	}
	directory, ok := file.(fs.ReadDirFile)
	if !ok {
		return file, nil
	}
	return &prunedDirectory{
		ReadDirFile: directory,
		allowHidden: p.dots.allows(depth(name)),
	}, nil
}

// dotPolicy reports the depths at which a dot-prefixed entry can become a
// match. A depth that the pattern cannot dot explicitly holds no selectable
// entry.
type dotPolicy struct {
	fixed map[int]struct{}
	flex  int
}

func newDotPolicy(pattern string) dotPolicy {
	policy := dotPolicy{fixed: make(map[int]struct{}), flex: -1}
	flexible := false
	consumed := 0
	for _, segment := range strings.Split(pattern, "/") {
		if segment == "**" {
			flexible = true
			continue
		}
		if explicitDotSegment(segment) {
			if flexible {
				if policy.flex < 0 || consumed < policy.flex {
					policy.flex = consumed
				}
			} else {
				policy.fixed[consumed] = struct{}{}
			}
		}
		consumed++
	}
	return policy
}

// allows reports whether a dot-prefixed entry at depth can become a match. A
// preceding ** shifts an explicit-dot segment to any later depth, so the
// policy allows every depth from the minimum.
func (p dotPolicy) allows(depth int) bool {
	if _, exists := p.fixed[depth]; exists {
		return true
	}
	return p.flex >= 0 && depth >= p.flex
}

// depth returns the component count of a relative fs path below the root.
func depth(name string) int {
	if name == "." || name == "" {
		return 0
	}
	return strings.Count(name, "/") + 1
}

type prunedDirectory struct {
	fs.ReadDirFile
	allowHidden bool
}

func (d *prunedDirectory) ReadDir(n int) ([]fs.DirEntry, error) {
	for {
		entries, err := d.ReadDirFile.ReadDir(n)
		kept := make([]fs.DirEntry, 0, len(entries))
		for _, entry := range entries {
			if d.retain(entry) {
				kept = append(kept, entry)
			}
		}
		if n <= 0 || len(kept) > 0 || err != nil {
			if errors.Is(err, io.EOF) && len(kept) > 0 {
				err = nil
			}
			return kept, err
		}
	}
}

// retain reports whether an entry stays visible to the walk.
func (d *prunedDirectory) retain(entry fs.DirEntry) bool {
	name := entry.Name()
	if reservedName(name) {
		return false
	}
	if !strings.HasPrefix(name, ".") {
		return true
	}
	return d.allowHidden
}

// reservedPath reports whether a path contains reserved state. A pattern never
// selects such a path, and the walk never enters it.
func reservedPath(path string) bool {
	for _, component := range strings.Split(path, "/") {
		if reservedName(component) {
			return true
		}
	}
	return false
}

func reservedName(name string) bool {
	switch strings.ToLower(name) {
	case ".lucy", ".git":
		return true
	default:
		return false
	}
}
