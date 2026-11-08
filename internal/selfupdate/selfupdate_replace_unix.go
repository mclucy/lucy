//go:build !windows

package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
)

// replaceBinary publishes the staged executable over the running one. Renaming
// over a running file is safe on Unix; the running process keeps the old inode.
func replaceBinary(staged, executable string) error {
	info, err := os.Lstat(executable)
	if err != nil {
		return fmt.Errorf("inspect %s: %w", executable, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", executable)
	}
	if err := os.Rename(staged, executable); err != nil {
		return fmt.Errorf("replace %s: %w", executable, err)
	}
	if dir, err := os.Open(filepath.Dir(executable)); err == nil {
		_ = dir.Sync()
		_ = dir.Close()
	}
	return nil
}
