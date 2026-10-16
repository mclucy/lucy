package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// MkdirAllPrivate persists newly created naming entries before callers publish data beneath them.
func MkdirAllPrivate(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	var missing []string
	for current := path; ; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err == nil {
			if !info.IsDir() {
				return fmt.Errorf("directory ancestor is not a directory: %s", current)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		missing = append(missing, current)
		if filepath.Dir(current) == current {
			return fmt.Errorf("filesystem root does not exist: %s: %w", current, err)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return makePrivateDirectories(path, missing)
}
