//go:build unix || darwin || linux

package server

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// WithInstanceLock validates name and holds a blocking, cross-process file lock
// while fn runs. It returns the callback's error, or a later close error if any.
func WithInstanceLock(name string, fn func() error) (err error) {
	if err := ValidateInstanceName(name); err != nil {
		return err
	}
	lockDir := filepath.Join(RunDir(), "locks")
	if err := ensureSharedDir(lockDir, 0o775); err != nil {
		return fmt.Errorf("create lock directory: %w", err)
	}
	file, err := os.OpenFile(filepath.Join(lockDir, name+".lock"), os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open instance lock: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close instance lock: %w", closeErr)
		}
	}()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock server %q: %w", name, err)
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return fn()
}
