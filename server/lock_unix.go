//go:build unix || darwin || linux

package server

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func WithInstanceLock(name string, fn func() error) error {
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
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock server %q: %w", name, err)
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return fn()
}
