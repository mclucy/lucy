package fsutil

import (
	"errors"
	"fmt"
	"os"
)

var ErrLocked = errors.New("file is locked")

// FileLock owns its file until Close. Do not copy a lock or close it concurrently.
type FileLock struct {
	file *os.File
}

// LockFile waits for exclusive ownership of a private lock file.
func LockFile(path string) (*FileLock, error) {
	return openLock(path, true)
}

// TryLockFile returns ErrLocked instead of waiting for another owner.
func TryLockFile(path string) (*FileLock, error) {
	return openLock(path, false)
}

func openLock(path string, wait bool) (*FileLock, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open lock: %w", err)
	}
	return LockOpenedFile(file, wait)
}

// LockOpenedFile takes ownership of file even if acquisition fails.
func LockOpenedFile(file *os.File, wait bool) (*FileLock, error) {
	if err := lockFile(file, wait); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return &FileLock{file: file}, nil
}

// Close releases ownership. A second Close has no effect.
func (l *FileLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	return errors.Join(unlockFile(file), file.Close())
}
