package document

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/internal/fsutil"
)

// Codec decodes and encodes one persisted document type.
type Codec[T any] struct {
	Decode func(io.Reader) (*T, error)
	Encode func(*T) ([]byte, error)
	// Merge writes value into an existing encoded document. It preserves the
	// comments the existing document carries, so a write never replaces user
	// comments. A nil Merge replaces the document with Encode.
	Merge func(existing []byte, value *T) ([]byte, error)
}

// Service reads and writes one document path. It does not lock; callers that
// need a transaction own the workspace lock.
type Service[T any] struct {
	path  string
	codec Codec[T]
	mode  os.FileMode
}

// NewService binds a codec to a path. New files use 0644 unless WithMode says
// otherwise.
func NewService[T any](path string, codec Codec[T]) Service[T] {
	return Service[T]{path: path, codec: codec, mode: 0o644}
}

// WithMode returns a copy of the service that creates new files with mode.
func (s Service[T]) WithMode(mode os.FileMode) Service[T] {
	s.mode = mode
	return s
}

// WithEncoder replaces the encoder. The encoder also replaces any existing
// document, because a caller that chooses its own bytes, such as a plain
// comment-free write, does not ask to preserve the previous document.
func (s Service[T]) WithEncoder(encode func(*T) ([]byte, error)) Service[T] {
	s.codec.Encode = encode
	s.codec.Merge = nil
	return s
}

// WithoutMerge returns a copy that always writes a fresh document.
func (s Service[T]) WithoutMerge() Service[T] {
	s.codec.Merge = nil
	return s
}

func (s Service[T]) Path() string { return s.path }

func (s Service[T]) Read() (*T, error) {
	file, err := os.Open(s.path)
	if err != nil {
		return nil, err
	}
	value, decodeErr := s.codec.Decode(file)
	return value, errors.Join(decodeErr, file.Close())
}

func (s Service[T]) Snapshot(value *T) ([]byte, error) {
	return s.codec.Encode(value)
}

func (s Service[T]) Write(value *T) error {
	data, err := s.writeBytes(value)
	if err != nil {
		return err
	}
	if err := fsutil.AtomicWrite(s.path, data, s.mode); err != nil {
		return err
	}
	return nil
}

// writeBytes merges value into the document the path already holds. A missing
// document is encoded fresh.
func (s Service[T]) writeBytes(value *T) ([]byte, error) {
	if s.codec.Merge == nil {
		return s.Snapshot(value)
	}
	existing, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return s.Snapshot(value)
	}
	if err != nil {
		return nil, err
	}
	return s.codec.Merge(existing, value)
}

// writeRootBytes merges value into the document relative names inside root. A
// missing document is encoded fresh.
func (s Service[T]) writeRootBytes(root *os.Root, relative string, value *T) ([]byte, error) {
	if s.codec.Merge == nil {
		return s.Snapshot(value)
	}
	file, err := root.Open(relative)
	if errors.Is(err, os.ErrNotExist) {
		return s.Snapshot(value)
	}
	if err != nil {
		return nil, err
	}
	existing, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		return nil, err
	}
	return s.codec.Merge(existing, value)
}

// Create publishes value only while the destination is still absent. A file
// that appears after the caller's check survives.
func (s Service[T]) Create(value *T) error {
	data, err := s.Snapshot(value)
	if err != nil {
		return err
	}
	parent := filepath.Dir(s.path)
	if err := fsutil.MkdirAllPrivate(parent); err != nil {
		return fmt.Errorf("create file parent: %w", err)
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return fmt.Errorf("open file parent: %w", err)
	}
	writeErr := fsutil.WriteRoot(
		root,
		filepath.Base(s.path),
		bytes.NewReader(data),
		fsutil.WriteOptions{Mode: s.mode, Replace: false},
	)
	if err := errors.Join(writeErr, root.Close()); err != nil {
		return err
	}
	return nil
}

// ReadRoot reads relative inside root. The root confines every path component
// to root, so a replaced parent cannot redirect the read.
func (s Service[T]) ReadRoot(root *os.Root, relative string) (*T, error) {
	file, err := root.Open(relative)
	if err != nil {
		return nil, err
	}
	value, decodeErr := s.codec.Decode(file)
	return value, errors.Join(decodeErr, file.Close())
}

// WriteRoot replaces relative inside root with value and retains the
// permissions of the file it replaces.
func (s Service[T]) WriteRoot(root *os.Root, relative string, value *T) error {
	return s.publishRoot(root, relative, value, true)
}

// CreateRoot publishes value at relative inside root only while the name is
// absent. A file that appears after the caller's check survives.
func (s Service[T]) CreateRoot(root *os.Root, relative string, value *T) error {
	return s.publishRoot(root, relative, value, false)
}

// publishRoot keeps every operation of one publish on the same root handle.
// Callers use it while they hold the writer lock that SafePath checked.
func (s Service[T]) publishRoot(root *os.Root, relative string, value *T, replace bool) error {
	var data []byte
	var err error
	if replace {
		data, err = s.writeRootBytes(root, relative, value)
	} else {
		data, err = s.Snapshot(value)
	}
	if err != nil {
		return err
	}
	mode := s.mode
	info, statErr := root.Lstat(relative)
	switch {
	case statErr == nil:
		if !info.Mode().IsRegular() {
			return fmt.Errorf("write destination must be a regular file")
		}
		mode = info.Mode().Perm()
	case !errors.Is(statErr, os.ErrNotExist):
		return statErr
	}
	err = fsutil.WriteRoot(root, relative, bytes.NewReader(data), fsutil.WriteOptions{
		Mode:    mode,
		Replace: replace,
	})
	if err != nil {
		return err
	}
	return nil
}

// ManifestFile returns the service for lucy.yaml in root.
func ManifestFile(root string) Service[Manifest] {
	service := NewService(filepath.Join(root, ManifestFilename), Codec[Manifest]{
		Decode: DecodeManifest,
		Encode: (*Manifest).CanonicalBytes,
		Merge: func(existing []byte, m *Manifest) ([]byte, error) {
			return m.MergeBytes(existing)
		},
	})
	return service
}

// LockFile returns the service for lucy-lock.yaml in root.
func LockFile(root string) Service[Lock] {
	service := NewService(filepath.Join(root, LockFilename), Codec[Lock]{
		Decode: DecodeLock,
		Encode: (*Lock).Snapshot,
		Merge: func(existing []byte, l *Lock) ([]byte, error) {
			return l.MergeBytes(existing)
		},
	})
	return service
}
