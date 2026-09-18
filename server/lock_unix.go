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
	if err := ensureSharedDir(lockDir, os.ModeSetgid|os.ModeSticky|0o775); err != nil {
		return fmt.Errorf("create lock directory: %w", err)
	}
	path := filepath.Join(lockDir, name+".lock")
	flags := os.O_RDWR | syscall.O_NOFOLLOW | syscall.O_NONBLOCK
	file, err := os.OpenFile(path, flags, 0)
	if os.IsNotExist(err) {
		file, err = os.OpenFile(path, flags|os.O_CREATE|os.O_EXCL, 0o660)
		if os.IsExist(err) {
			file, err = os.OpenFile(path, flags, 0)
		}
	}
	if err != nil {
		return fmt.Errorf("open instance lock: %w", err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("close instance lock: %w", closeErr)
		}
	}()
	if err := prepareLockFile(file, lockDir); err != nil {
		return err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("lock server %q: %w", name, err)
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return fn()
}

// prepareLockFile upgrades legacy permissions through the opened descriptor,
// rejecting special files and hard links before any ownership or mode changes.
func prepareLockFile(file *os.File, lockDir string) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat instance lock: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 {
		return fmt.Errorf("instance lock must be a regular file with one link")
	}
	dir, err := os.Stat(lockDir)
	if err != nil {
		return fmt.Errorf("stat lock directory: %w", err)
	}
	dirStat, ok := dir.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot determine lock directory group")
	}
	if stat.Gid != dirStat.Gid {
		if err := file.Chown(-1, int(dirStat.Gid)); err != nil {
			return fmt.Errorf("update instance lock group (run as its owner or root): %w", err)
		}
	}
	if info.Mode().Perm() != 0o660 {
		if err := file.Chmod(0o660); err != nil {
			return fmt.Errorf("update instance lock permissions (run as its owner or root): %w", err)
		}
	}
	return nil
}
