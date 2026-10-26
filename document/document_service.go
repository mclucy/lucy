package document

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/log"
)

// Codec decodes and encodes one persisted document type.
type Codec[T any] struct {
	Decode func(io.Reader) (*T, error)
	Encode func(*T) ([]byte, error)
}

// Service reads and writes one document path. It does not lock; callers that
// need a transaction own the workspace lock.
type Service[T any] struct {
	path  string
	codec Codec[T]
	kind  string
	mode  os.FileMode
}

// NewService binds a codec to a path. New files use 0644 unless WithMode says
// otherwise.
func NewService[T any](path string, codec Codec[T]) Service[T] {
	return Service[T]{path: path, codec: codec, kind: "document", mode: 0o644}
}

// WithMode returns a copy of the service that creates new files with mode.
func (s Service[T]) WithMode(mode os.FileMode) Service[T] {
	s.mode = mode
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
	data, err := s.Snapshot(value)
	if err != nil {
		return err
	}
	if err := fsutil.AtomicWrite(s.path, data, s.mode); err != nil {
		return err
	}
	log.Info(s.kind+" written", "path", s.path)
	return nil
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
	log.Info(s.kind+" created", "path", s.path)
	return nil
}

// ManifestFile returns the service for lucy.yaml in root.
func ManifestFile(root string) Service[Manifest] {
	service := NewService(filepath.Join(root, ManifestFilename), Codec[Manifest]{
		Decode: DecodeManifest,
		Encode: (*Manifest).CanonicalBytes,
	})
	service.kind = "manifest"
	return service
}

// LockFile returns the service for lucy-lock.yaml in root.
func LockFile(root string) Service[Lock] {
	service := NewService(filepath.Join(root, LockFilename), Codec[Lock]{
		Decode: DecodeLock,
		Encode: (*Lock).Snapshot,
	})
	service.kind = "lock"
	return service
}
