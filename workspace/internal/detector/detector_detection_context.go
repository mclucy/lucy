package detector

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

type DetectionContext struct {
	root      string
	files     map[string]*DetectionFile
	rootFiles []*DetectionFile
}

type DetectionFile struct {
	path        string
	relPath     string
	isDir       bool
	dataOnce    sync.Once
	data        []byte
	dataErr     error
	archiveOnce sync.Once
	archive     *zip.Reader
	archiveErr  error
}

func NewDetectionContext(root string) (DetectionContext, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return DetectionContext{}, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return DetectionContext{}, err
	}

	context := DetectionContext{
		root:      root,
		files:     make(map[string]*DetectionFile, len(entries)),
		rootFiles: make([]*DetectionFile, 0, len(entries)),
	}
	for _, entry := range entries {
		file := &DetectionFile{
			path:    filepath.Join(root, entry.Name()),
			relPath: filepath.ToSlash(entry.Name()),
			isDir:   entry.IsDir(),
		}
		context.files[file.relPath] = file
		context.rootFiles = append(context.rootFiles, file)
	}
	return context, nil
}

func NewDetectionFile(path string) *DetectionFile {
	path = filepath.Clean(path)
	info, err := os.Stat(path)
	return &DetectionFile{
		path:    path,
		relPath: filepath.ToSlash(filepath.Base(path)),
		isDir:   err == nil && info.IsDir(),
	}
}

func (c DetectionContext) RootFiles() []*DetectionFile {
	return append([]*DetectionFile(nil), c.rootFiles...)
}

func (c DetectionContext) RootFile(path string) (*DetectionFile, bool) {
	if filepath.IsAbs(path) {
		rel, err := filepath.Rel(c.root, filepath.Clean(path))
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return nil, false
		}
		path = rel
	}
	path = filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	file, ok := c.files[path]
	return file, ok
}

func (c DetectionContext) Sibling(primary *DetectionFile, name string) (*DetectionFile, bool) {
	if primary == nil {
		return nil, false
	}
	return c.RootFile(filepath.Join(filepath.Dir(primary.RelPath()), name))
}

func (f *DetectionFile) Path() string {
	if f == nil {
		return ""
	}
	return f.path
}

func (f *DetectionFile) RelPath() string {
	if f == nil {
		return ""
	}
	return f.relPath
}

func (f *DetectionFile) IsDirectory() bool {
	return f != nil && f.isDir
}

func (f *DetectionFile) Read() ([]byte, error) {
	if f == nil {
		return nil, os.ErrInvalid
	}
	f.dataOnce.Do(func() {
		f.data, f.dataErr = os.ReadFile(f.path)
	})
	return f.data, f.dataErr
}

func (f *DetectionFile) Archive() (*zip.Reader, error) {
	if f == nil {
		return nil, os.ErrInvalid
	}
	if f.isDir {
		return nil, nil
	}
	f.archiveOnce.Do(func() {
		data, err := f.Read()
		if err != nil {
			f.archiveErr = err
			return
		}
		f.archive, f.archiveErr = zip.NewReader(bytes.NewReader(data), int64(len(data)))
	})
	return f.archive, f.archiveErr
}
