package fsutil

import (
	"errors"
	"fmt"
	"os"
)

var ErrLocked = errors.New("file is locked")

// FileLock owns its file until Close. Do not copy a lock or close it concurrently.
type FileLock struct {
	file  *os.File
	flush bool
}

// FlushOnClose makes Close Flush the device of the lock file before it
// releases the lock, so the writes of an owner that reports success survive
// a power loss. Writes under the lock then need only Barrier.
func (l *FileLock) FlushOnClose() *FileLock {
	l.flush = true
	return l
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
	file, err := openLockFile(path)
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

func (l *FileLock) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	file := l.file
	l.file = nil
	var flushErr error
	if l.flush {
		flushErr = Flush(file)
	}
	return errors.Join(flushErr, unlockFile(file), file.Close())
}
