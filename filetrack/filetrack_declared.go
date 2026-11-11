package filetrack

import (
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/input"
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
	declared := make(map[string]documentpkg.TrackedFile, len(files))
	for _, file := range files {
		declared[file.Path] = file
	}
	tree := newDeclarationTree(files)
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
		err := doublestar.GlobWalk(
			tree,
			pattern,
			func(name string, _ fs.DirEntry) error {
				matched = true
				if _, explicit := literal[name]; explicit {
					return nil
				}
				file, exists := declared[name]
				if !exists {
					return nil
				}
				if operation == Recover && file.Hash == "" {
					addSkipped(name)
					return nil
				}
				addSelected(name)
				return nil
			},
			doublestar.WithFilesOnly(),
			doublestar.WithNoHidden(),
		)
		if err != nil {
			return nil, nil, err
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

// declarationTree exposes tracked paths to doublestar as an in-memory
// filesystem, so declaration matching inherits WithNoHidden and globstar
// semantics without walking the workspace.
type declarationTree struct {
	children map[string][]declarationEntry
	stats    map[string]declarationEntry
}

type declarationEntry struct {
	name string
	mode fs.FileMode
}

func newDeclarationTree(files []documentpkg.TrackedFile) *declarationTree {
	tree := &declarationTree{
		children: make(map[string][]declarationEntry),
		stats: map[string]declarationEntry{
			".": {name: ".", mode: fs.ModeDir},
		},
	}
	tree.children["."] = nil
	for _, file := range files {
		components := strings.Split(file.Path, "/")
		parent := "."
		for _, component := range components[:len(components)-1] {
			child := path.Join(parent, component)
			if _, exists := tree.stats[child]; !exists {
				entry := declarationEntry{name: component, mode: fs.ModeDir}
				tree.stats[child] = entry
				tree.children[parent] = append(tree.children[parent], entry)
				tree.children[child] = nil
			}
			parent = child
		}
		name := components[len(components)-1]
		entry := declarationEntry{name: name}
		tree.stats[file.Path] = entry
		tree.children[parent] = append(tree.children[parent], entry)
	}
	for parent := range tree.children {
		slices.SortFunc(
			tree.children[parent],
			func(left, right declarationEntry) int {
				return strings.Compare(left.name, right.name)
			},
		)
	}
	return tree
}

func (t *declarationTree) Open(name string) (fs.File, error) {
	entry, exists := t.stats[name]
	if !exists {
		return nil, fs.ErrNotExist
	}
	return &declarationFile{tree: t, name: name, entry: entry}, nil
}

func (t *declarationTree) ReadDir(name string) ([]fs.DirEntry, error) {
	children, exists := t.children[name]
	if !exists {
		return nil, fs.ErrNotExist
	}
	entries := make([]fs.DirEntry, len(children))
	for index := range children {
		entries[index] = children[index]
	}
	return entries, nil
}

func (t *declarationTree) Stat(name string) (fs.FileInfo, error) {
	entry, exists := t.stats[name]
	if !exists {
		return nil, fs.ErrNotExist
	}
	return entry, nil
}

type declarationFile struct {
	tree  *declarationTree
	name  string
	entry declarationEntry
}

func (f *declarationFile) Read([]byte) (int, error) { return 0, io.EOF }

func (f *declarationFile) Close() error { return nil }

func (f *declarationFile) Stat() (fs.FileInfo, error) { return f.entry, nil }

func (f *declarationFile) ReadDir(count int) ([]fs.DirEntry, error) {
	entries, err := f.tree.ReadDir(f.name)
	if err != nil {
		return nil, err
	}
	if count <= 0 || count > len(entries) {
		return entries, nil
	}
	return entries[:count], nil
}

func (e declarationEntry) Name() string { return e.name }

func (e declarationEntry) Size() int64 { return 0 }

func (e declarationEntry) Mode() fs.FileMode { return e.mode }

func (e declarationEntry) ModTime() time.Time { return time.Time{} }

func (e declarationEntry) IsDir() bool { return e.mode.IsDir() }

func (e declarationEntry) Sys() any { return nil }

func (e declarationEntry) Type() fs.FileMode { return e.mode.Type() }

func (e declarationEntry) Info() (fs.FileInfo, error) { return e, nil }
