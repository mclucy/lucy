package install

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows"
)

func LockWorkspace(root string) (*os.File, error) {
	// The lock file carries no data; a read-only handle is enough to lock it.
	f, err := os.OpenFile(filepath.Join(root, ".lucy", "writer.lock"), os.O_CREATE|os.O_RDONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &windows.Overlapped{}); err != nil {
		f.Close()
		return nil, fmt.Errorf("another Lucy operation owns this workspace: %w", err)
	}
	return f, nil
}
