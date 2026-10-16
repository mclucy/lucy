//go:build darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris

package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func lockFile(file *os.File, wait bool) error {
	flags := unix.LOCK_EX
	if !wait {
		flags |= unix.LOCK_NB
	}
	err := unix.Flock(int(file.Fd()), flags)
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return fmt.Errorf("lock file: %w: %w", ErrLocked, err)
	}
	return err
}

func unlockFile(file *os.File) error {
	return unix.Flock(int(file.Fd()), unix.LOCK_UN)
}

func renameRoot(root *os.Root, from, to string, replace bool) error {
	if !replace {
		if err := root.Link(from, to); err != nil {
			return fmt.Errorf("publish absent name in root: %w", err)
		}
		if err := syncRenameChain(root, to); err != nil {
			return err
		}
		if err := root.Remove(from); err != nil {
			return err
		}
		return syncRenameChain(root, from)
	}
	if err := root.Rename(from, to); err != nil {
		return fmt.Errorf("rename in root: %w", err)
	}
	return syncRenameChain(root, from, to)
}

// sameDevice reports whether both paths live on one filesystem, so that a
// staged copy reaches its destination through a rename. When either path
// cannot be stated, it reports false and the publish stages beside the
// destination instead, which is always on the destination filesystem.
func sameDevice(first, second string) bool {
	var firstStat, secondStat unix.Stat_t
	if err := unix.Stat(first, &firstStat); err != nil {
		return false
	}
	if err := unix.Stat(second, &secondStat); err != nil {
		return false
	}
	return firstStat.Dev == secondStat.Dev
}

// syncRenameChain flushes the directory entries that a rename changes: the
// parent directories of both endpoints and every directory from those parents
// up to root.
func syncRenameChain(root *os.Root, names ...string) error {
	var resultErr error
	for _, name := range names {
		for dir := filepath.Dir(name); ; dir = filepath.Dir(dir) {
			resultErr = errors.Join(resultErr, syncDir(root, dir))
			if dir == "." {
				break
			}
		}
	}
	return resultErr
}

func syncDir(root *os.Root, dir string) error {
	file, err := root.OpenFile(dir, os.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("open directory for sync: %w", err)
	}
	var resultErr error
	if err := file.Sync(); err != nil {
		resultErr = fmt.Errorf("sync directory: %w", err)
	}
	if err := file.Close(); err != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("close directory: %w", err))
	}
	return resultErr
}

func makePrivateDirectories(path string, missing []string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	for _, directory := range missing {
		parent, err := os.Open(filepath.Dir(directory))
		if err != nil {
			return err
		}
		syncErr := parent.Sync()
		closeErr := parent.Close()
		if syncErr != nil || closeErr != nil {
			return errors.Join(syncErr, closeErr)
		}
	}
	return nil
}
