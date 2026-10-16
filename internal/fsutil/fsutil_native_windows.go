//go:build windows

package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func lockFile(file *os.File, wait bool) error {
	flags := uint32(windows.LOCKFILE_EXCLUSIVE_LOCK)
	if !wait {
		flags |= windows.LOCKFILE_FAIL_IMMEDIATELY
	}
	err := windows.LockFileEx(
		windows.Handle(file.Fd()),
		flags,
		0,
		1,
		0,
		&windows.Overlapped{},
	)
	if err != nil && isLockContention(err) {
		return fmt.Errorf("lock file: %w: %w", ErrLocked, err)
	}
	return err
}

func unlockFile(file *os.File) error {
	return windows.UnlockFileEx(
		windows.Handle(file.Fd()),
		0,
		1,
		0,
		&windows.Overlapped{},
	)
}

func isLockContention(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION) ||
		errors.Is(err, windows.ERROR_SHARING_VIOLATION)
}

// sameDevice reports whether both paths live on one filesystem. Windows
// volumes make the check unreliable through reparse points, so a publish
// always stages beside its destination instead.
func sameDevice(first, second string) bool {
	return false
}

func renameRoot(root *os.Root, from, to string, replace bool) (resultErr error) {
	sourceRoot, err := root.OpenRoot(filepath.Dir(from))
	if err != nil {
		return fmt.Errorf("open staged directory: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, sourceRoot.Close()) }()
	file, err := sourceRoot.Open(filepath.Base(from))
	if err != nil {
		return err
	}
	sourcePath, pathErr := finalDosPath(windows.Handle(file.Fd()))
	if closeErr := file.Close(); pathErr != nil || closeErr != nil {
		return errors.Join(pathErr, closeErr)
	}
	targetRoot, err := root.OpenRoot(filepath.Dir(to))
	if err != nil {
		return fmt.Errorf("open target directory: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, targetRoot.Close()) }()
	directory, err := targetRoot.Open(".")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	targetPath, err := finalDosPath(windows.Handle(directory.Fd()))
	if err != nil {
		return err
	}
	source, err := windows.UTF16PtrFromString(sourcePath)
	if err != nil {
		return err
	}
	destination, err := windows.UTF16PtrFromString(filepath.Join(targetPath, filepath.Base(to)))
	if err != nil {
		return err
	}
	attributes, attributeErr := windows.GetFileAttributes(destination)
	if attributeErr != nil && !errors.Is(attributeErr, windows.ERROR_FILE_NOT_FOUND) && !errors.Is(attributeErr, windows.ERROR_PATH_NOT_FOUND) {
		return attributeErr
	}
	readOnly := replace && attributeErr == nil && attributes&windows.FILE_ATTRIBUTE_READONLY != 0
	if readOnly {
		if err := windows.SetFileAttributes(destination, attributes&^windows.FILE_ATTRIBUTE_READONLY); err != nil {
			return err
		}
	}
	flags := uint32(windows.MOVEFILE_WRITE_THROUGH)
	if replace {
		flags |= windows.MOVEFILE_REPLACE_EXISTING
	}
	if err := windows.MoveFileEx(source, destination, flags); err != nil {
		var restoreErr error
		if readOnly {
			restoreErr = windows.SetFileAttributes(destination, attributes)
		}
		return errors.Join(fmt.Errorf("publish file: %w", err), restoreErr)
	}
	return nil
}

// finalDosPath resolves handle to its canonical \\?\ volume path. The API
// returns the required size instead of failing when the first buffer is small.
func finalDosPath(handle windows.Handle) (string, error) {
	buf := make([]uint16, windows.MAX_PATH+1)
	for {
		n, err := windows.GetFinalPathNameByHandle(handle, &buf[0], uint32(len(buf)), 0)
		if err != nil {
			return "", err
		}
		if int(n) < len(buf) {
			return windows.UTF16PtrToString(&buf[0]), nil
		}
		buf = make([]uint16, n+1)
	}
}

func makePrivateDirectories(path string, missing []string) error {
	for i := len(missing) - 1; i >= 0; i-- {
		directory := missing[i]
		if info, err := os.Stat(directory); err == nil && info.IsDir() {
			continue
		}
		staged, err := os.MkdirTemp(filepath.Dir(directory), ".lucy-directory-")
		if err != nil {
			return err
		}
		source, sourceErr := windows.UTF16PtrFromString(staged)
		destination, destinationErr := windows.UTF16PtrFromString(directory)
		if sourceErr != nil || destinationErr != nil {
			return errors.Join(sourceErr, destinationErr, os.Remove(staged))
		}
		if err := windows.MoveFileEx(source, destination, windows.MOVEFILE_WRITE_THROUGH); err != nil {
			cleanupErr := os.Remove(staged)
			if errors.Is(err, windows.ERROR_ALREADY_EXISTS) || errors.Is(err, windows.ERROR_FILE_EXISTS) {
				if info, statErr := os.Stat(directory); statErr == nil && info.IsDir() {
					if cleanupErr != nil {
						return cleanupErr
					}
					continue
				}
			}
			return errors.Join(err, cleanupErr)
		}
	}
	return nil
}
