package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type store struct {
	dir string
}

func newStore(dir string) *store {
	return &store{dir: dir}
}

func (s *store) Write(contentHash, filename string, data []byte) error {
	filename = sanitizeFilename(filename, contentHash)
	dir := filepath.Join(s.dir, contentHash)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create blob directory: %w", err)
	}
	filePath := filepath.Join(dir, filename)
	if !containedUnder(dir, filePath) {
		return fmt.Errorf("filename %q escapes cache directory", filename)
	}
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write blob: %w", err)
	}
	return nil
}

// Read opens the blob and returns the file handle. Caller must close it.
func (s *store) Read(contentHash, filename string) (*os.File, error) {
	p := filepath.Join(s.dir, contentHash, filename)
	f, err := os.Open(p)
	if err != nil {
		return nil, fmt.Errorf("failed to open blob: %w", err)
	}
	return f, nil
}

func (s *store) Remove(contentHash string) error {
	p := filepath.Join(s.dir, contentHash)
	if err := os.RemoveAll(p); err != nil {
		return fmt.Errorf("failed to remove blob: %w", err)
	}
	return nil
}

// sanitizeFilename prevents path traversal by stripping directory components.
func sanitizeFilename(name, fallback string) string {
	name = filepath.Base(name)
	if name == "." || name == "/" || name == string(filepath.Separator) {
		return fallback
	}
	return name
}

// containedUnder validates child is strictly inside parent (prevents path traversal).
func containedUnder(parent, child string) bool {
	absParent, err := filepath.Abs(parent)
	if err != nil {
		return false
	}
	absChild, err := filepath.Abs(child)
	if err != nil {
		return false
	}
	return strings.HasPrefix(absChild, absParent+string(filepath.Separator))
}
