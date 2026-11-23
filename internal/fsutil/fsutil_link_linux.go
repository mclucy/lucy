//go:build linux

package fsutil

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// cloneAt creates name in dir as a copy-on-write clone of src with perm. It
// reports an error and leaves name absent when the filesystem cannot clone.
func cloneAt(src string, dir *os.File, name string, perm os.FileMode) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()
	fd, err := unix.Openat(
		int(dir.Fd()),
		name,
		unix.O_CREAT|unix.O_EXCL|unix.O_WRONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC,
		0o600,
	)
	if err != nil {
		return err
	}
	target := os.NewFile(uintptr(fd), name)
	err = unix.IoctlFileClone(fd, int(source.Fd()))
	if err == nil {
		err = target.Chmod(perm)
	}
	if err = errors.Join(err, target.Close()); err != nil {
		return errors.Join(err, unix.Unlinkat(int(dir.Fd()), name, 0))
	}
	return nil
}
