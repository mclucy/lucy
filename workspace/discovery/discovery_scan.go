package discovery

import (
	"archive/zip"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/mclucy/lucy/internal/fsutil"
)

// scan owns one canonical root, the root-confined filesystem view, and the
// artifact metadata cache for a single Discover call. It is private: no
// resource-bearing value escapes into Result.
type scan struct {
	root        string
	displayRoot string
	fs          *os.Root
	mu          sync.Mutex
	files       map[string]*artifact
	physical    []physicalArtifact
	rootFiles   []*artifact
	readers     []*os.File
	closed      bool
}

type physicalArtifact struct {
	info os.FileInfo
	file *artifact
}

// artifact is one cached filesystem entry. Sidecar bytes and ZIP metadata are
// cached lazily; a JAR is never cached as a whole byte slice.
type artifact struct {
	scan    *scan
	path    string
	relPath string
	isDir   bool
	size    int64

	dataOnce sync.Once
	data     []byte
	dataErr  error

	archiveOnce sync.Once
	archive     *zip.Reader
	archiveErr  error
}

func newScan(root string) (*scan, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("scan root %s is not a directory", resolved)
	}
	fs, err := os.OpenRoot(resolved)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(resolved)
	if err != nil {
		_ = fs.Close()
		return nil, err
	}

	s := &scan{
		root:        resolved,
		displayRoot: absolute,
		fs:          fs,
		files:       make(map[string]*artifact, len(entries)),
		rootFiles:   make([]*artifact, 0, len(entries)),
	}
	rootSeen := make(map[*artifact]bool, len(entries))
	for _, entry := range entries {
		file, ok := s.fileLocked(entry.Name())
		if !ok || rootSeen[file] {
			continue
		}
		rootSeen[file] = true
		s.rootFiles = append(s.rootFiles, file)
	}
	return s, nil
}

// RootFiles returns the cached top-level artifacts. The slice is the scan's
// internal read-only view; callers must not mutate or retain it past the scan.
func (s *scan) RootFiles() []*artifact {
	if s == nil {
		return nil
	}
	return s.rootFiles
}

// File resolves an absolute or root-relative path inside the root. Nested
// paths and symlinks are accepted only when their resolved target stays inside
// the root.
func (s *scan) File(path string) (*artifact, bool) {
	if s == nil {
		return nil, false
	}
	rel, ok := s.relative(path)
	if !ok {
		return nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.fileLocked(rel)
}

// fileLocked resolves one path to the single cached artifact that represents
// its physical file. Aliases, hard links, and symlinks that resolve inside the
// root share one artifact, one metadata cache, and one ReaderAt descriptor.
func (s *scan) fileLocked(rel string) (*artifact, bool) {
	if file, exists := s.files[rel]; exists {
		return file, true
	}
	if s.closed {
		return nil, false
	}
	linkInfo, err := s.fs.Lstat(rel)
	if err != nil {
		return nil, false
	}
	info, statErr := s.fs.Stat(rel)
	canonicalRel := rel
	if statErr != nil {
		// A symlink that escapes the root is not followed. It remains its own
		// unreadable artifact so a later read fails instead of leaking data.
		info = linkInfo
	} else if linkInfo.Mode()&os.ModeSymlink != 0 {
		if resolved, resolveErr := filepath.EvalSymlinks(
			filepath.Join(s.displayRoot, rel),
		); resolveErr == nil {
			if target, ok := relativeWithin(s.root, resolved); ok {
				canonicalRel = target
			} else if target, ok := relativeWithin(s.displayRoot, resolved); ok {
				canonicalRel = target
			}
		}
	}

	for _, physical := range s.physical {
		if !os.SameFile(physical.info, info) {
			continue
		}
		s.files[rel] = physical.file
		if canonicalRel != rel {
			s.files[canonicalRel] = physical.file
		}
		return physical.file, true
	}

	file := &artifact{
		scan:    s,
		path:    filepath.Join(s.displayRoot, canonicalRel),
		relPath: canonicalRel,
		isDir:   info.IsDir(),
	}
	if !info.IsDir() {
		file.size = info.Size()
	}
	s.physical = append(s.physical, physicalArtifact{info: info, file: file})
	s.files[rel] = file
	if canonicalRel != rel {
		s.files[canonicalRel] = file
	}
	return file, true
}

// Sibling resolves a named sibling of primary inside the root.
func (s *scan) Sibling(primary *artifact, name string) (*artifact, bool) {
	if s == nil || primary == nil || name == "" {
		return nil, false
	}
	return s.File(filepath.Join(filepath.Dir(primary.RelPath()), name))
}

// ReadDir lists a root-confined directory in lexical order.
func (s *scan) ReadDir(path string) ([]os.DirEntry, error) {
	if s == nil {
		return nil, os.ErrInvalid
	}
	rel, ok := s.relative(path)
	if !ok {
		return nil, fmt.Errorf("path %q is outside the scan root", path)
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return nil, os.ErrClosed
	}
	file, err := s.fs.Open(rel)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	entries, err := file.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(entries, func(left, right os.DirEntry) int {
		return strings.Compare(left.Name(), right.Name())
	})
	return entries, nil
}

// Close releases every ReaderAt descriptor and the root handle.
func (s *scan) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	var problems []error
	for _, reader := range s.readers {
		if err := reader.Close(); err != nil {
			problems = append(problems, err)
		}
	}
	s.readers = nil
	if s.fs != nil {
		if err := s.fs.Close(); err != nil {
			problems = append(problems, err)
		}
		s.fs = nil
	}
	return errors.Join(problems...)
}

func (s *scan) relative(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	clean := filepath.Clean(path)
	if filepath.IsAbs(clean) {
		if rel, ok := relativeWithin(s.displayRoot, clean); ok {
			return rel, true
		}
		if rel, ok := relativeWithin(s.root, clean); ok {
			return rel, true
		}
		resolved, err := filepath.EvalSymlinks(clean)
		if err == nil {
			if rel, ok := relativeWithin(s.root, resolved); ok {
				return rel, true
			}
		}
		return "", false
	}
	if clean == "." || !filepath.IsLocal(clean) {
		return "", false
	}
	return clean, true
}

func relativeWithin(root, path string) (string, bool) {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return "", false
	}
	return rel, true
}

func (s *scan) openReader(file *artifact) (*os.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, os.ErrClosed
	}
	reader, err := s.fs.Open(file.relPath)
	if err != nil {
		return nil, err
	}
	s.readers = append(s.readers, reader)
	return reader, nil
}

// Path returns the absolute canonical path.
func (a *artifact) Path() string {
	if a == nil {
		return ""
	}
	return a.path
}

// RelPath returns the path relative to the scan root using slashes.
func (a *artifact) RelPath() string {
	if a == nil {
		return ""
	}
	return filepath.ToSlash(a.relPath)
}

// IsDirectory reports whether the artifact is a directory.
func (a *artifact) IsDirectory() bool {
	return a != nil && a.isDir
}

// Read returns bounded sidecar bytes. JAR/ZIP payloads must be opened through
// Archive so a large server JAR is never cached as a whole byte slice.
func (a *artifact) Read() ([]byte, error) {
	if a == nil {
		return nil, os.ErrInvalid
	}
	a.dataOnce.Do(func() {
		if a.isDir {
			a.dataErr = errNotRegularFile
			return
		}
		if isArchivePath(a.relPath) {
			a.dataErr = fmt.Errorf(
				"artifact %s is an archive; open it through Archive",
				a.RelPath(),
			)
			return
		}
		if a.size > fsutil.MaxZipEntryBytes {
			a.dataErr = fmt.Errorf(
				"artifact %s exceeds the sidecar read limit",
				a.RelPath(),
			)
			return
		}
		if a.scan == nil || a.scan.fs == nil {
			a.dataErr = os.ErrClosed
			return
		}
		reader, err := a.scan.fs.Open(a.relPath)
		if err != nil {
			a.dataErr = err
			return
		}
		defer func() {
			if err := reader.Close(); err != nil {
				a.dataErr = err
			}
		}()
		a.data, a.dataErr = fsutil.CopyBytes(reader, fsutil.MaxZipEntryBytes)
	})
	return a.data, a.dataErr
}

// Archive opens the artifact as a ZIP reader backed by a scan-owned ReaderAt
// descriptor. The descriptor stays open until scan.Close.
func (a *artifact) Archive() (*zip.Reader, error) {
	if a == nil {
		return nil, os.ErrInvalid
	}
	a.archiveOnce.Do(func() {
		if a.isDir {
			a.archiveErr = fmt.Errorf("artifact %s is a directory", a.RelPath())
			return
		}
		if a.scan == nil {
			a.archiveErr = os.ErrInvalid
			return
		}
		reader, err := a.scan.openReader(a)
		if err != nil {
			a.archiveErr = err
			return
		}
		info, err := reader.Stat()
		if err != nil {
			a.archiveErr = err
			return
		}
		a.archive, a.archiveErr = zip.NewReader(reader, info.Size())
	})
	return a.archive, a.archiveErr
}

func isArchivePath(path string) bool {
	lower := strings.ToLower(path)
	return strings.HasSuffix(lower, ".jar") || strings.HasSuffix(lower, ".zip")
}
