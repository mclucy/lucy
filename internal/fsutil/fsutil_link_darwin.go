//go:build darwin

package fsutil

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// cloneAt creates name in dir as a copy-on-write clone of src with perm. It
// reports an error and leaves name absent when the filesystem cannot clone.
func cloneAt(src string, dir *os.File, name string, perm os.FileMode) error {
	fd := int(dir.Fd())
	if err := unix.Clonefileat(unix.AT_FDCWD, src, fd, name, unix.CLONE_NOFOLLOW); err != nil {
		return err
	}
	if err := unix.Fchmodat(fd, name, uint32(perm), unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return errors.Join(err, unix.Unlinkat(fd, name, 0))
	}
	return nil
}
