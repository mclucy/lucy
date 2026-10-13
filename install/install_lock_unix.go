//go:build !windows

package install

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

func lockWorkspace(root string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(root, ".lucy", "writer.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, fmt.Errorf("another Lucy operation owns this workspace: %w", err)
	}
	return f, nil
}
