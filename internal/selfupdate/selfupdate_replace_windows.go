//go:build windows

package selfupdate

import (
	"fmt"
	"os"
	"time"
)

// replaceBinary moves the running image aside, publishes the staged binary at
// the original path, and removes the previous file when the process exits.
func replaceBinary(staged, executable string) error {
	info, err := os.Lstat(executable)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", executable, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", executable)
	}

	backup := executable + ".old"
	_ = os.Remove(backup)
	if err := renameWithRetry(executable, backup); err != nil {
		return fmt.Errorf("move the running lucy aside: %w", err)
	}
	if err := renameWithRetry(staged, executable); err != nil {
		_ = os.Rename(backup, executable)
		return fmt.Errorf("install the new lucy: %w", err)
	}
	// The previous image stays mapped until this process exits, so removal can
	// fail here; the next update deletes it before moving the next image aside.
	_ = os.Remove(backup)
	return nil
}

func renameWithRetry(from, to string) error {
	var err error
	for attempt := 1; attempt <= 5; attempt++ {
		err = os.Rename(from, to)
		if err == nil {
			return nil
		}
		time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
	}
	return err
}
