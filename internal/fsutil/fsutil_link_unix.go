//go:build unix

package fsutil

import (
	"os"

	"golang.org/x/sys/unix"
)

// linkAt creates name in dir as a hardlink of src. name must be absent.
func linkAt(src string, dir *os.File, name string) error {
	return unix.Linkat(unix.AT_FDCWD, src, int(dir.Fd()), name, 0)
}

// hardlinkSharesInode reports whether LinkHardlink can deploy a destination
// that shares the stored inode on this platform.
const hardlinkSharesInode = true
