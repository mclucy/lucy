package fsutil

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// RenameRoot publishes a closed, synced file inside root and persists the rename.
// Parent directories must exist. Platform implementations preserve root confinement.
func RenameRoot(root *os.Root, from, to string) error {
	return publishRoot(root, from, to, true)
}

// RenameRootNoReplace publishes only if the destination name is still absent.
func RenameRootNoReplace(root *os.Root, from, to string) error {
	return publishRoot(root, from, to, false)
}

func publishRoot(root *os.Root, from, to string, replace bool) error {
	if !filepath.IsLocal(from) || !filepath.IsLocal(to) {
		return fmt.Errorf("rename paths must stay inside their root")
	}
	from = filepath.Clean(from)
	to = filepath.Clean(to)
	for _, name := range []string{from, to} {
		info, err := root.Lstat(name)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("rename endpoint must be a regular file")
		}
		for parent := filepath.Dir(name); parent != "."; parent = filepath.Dir(parent) {
			info, err := root.Lstat(parent)
			if err != nil {
				return err
			}
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("rename parent must be a regular directory")
			}
		}
	}
	return renameRoot(root, from, to, replace)
}

// AtomicWrite replaces a file atomically and retains its permissions.
// New files use defaultMode.
func AtomicWrite(path string, data []byte, defaultMode os.FileMode) error {
	mode := defaultMode
	info, err := os.Lstat(path)
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("write destination must be a regular file")
		}
		mode = info.Mode().Perm()
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect file permissions: %w", err)
	}
	return atomicCopy(bytes.NewReader(data), path, mode)
}

// CopyFileAtomic discards incomplete copies and returns the published file at offset zero.
func CopyFileAtomic(reader io.Reader, path string, mode os.FileMode) (*os.File, error) {
	if err := atomicCopy(reader, path, mode); err != nil {
		return nil, err
	}
	return os.Open(path)
}

func atomicCopy(reader io.Reader, path string, mode os.FileMode) (resultErr error) {
	parent := filepath.Dir(path)
	if err := MkdirAllPrivate(parent); err != nil {
		return fmt.Errorf("create file parent: %w", err)
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return fmt.Errorf("open file parent: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	name := ".lucy-write-" + rand.Text()
	file, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create staged file: %w", err)
	}
	defer func() {
		if file != nil {
			resultErr = errors.Join(resultErr, file.Close())
		}
		if err := root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
			resultErr = errors.Join(resultErr, fmt.Errorf("remove staged file: %w", err))
		}
	}()
	if _, err := io.Copy(file, reader); err != nil {
		return fmt.Errorf("write staged file: %w", err)
	}
	if err := file.Chmod(mode.Perm()); err != nil {
		return fmt.Errorf("set staged permissions: %w", err)
	}
	if err := file.Sync(); err != nil {
		return fmt.Errorf("sync staged file: %w", err)
	}
	if err := file.Close(); err != nil {
		file = nil
		return fmt.Errorf("close staged file: %w", err)
	}
	file = nil
	if err := RenameRoot(root, name, filepath.Base(path)); err != nil {
		return fmt.Errorf("publish staged file: %w", err)
	}
	return nil
}
