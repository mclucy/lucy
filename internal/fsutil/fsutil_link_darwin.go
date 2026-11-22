//go:build darwin

package fsutil

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// cloneFile places a copy-on-write clone of src at dst with perm. It reports
// an error and leaves dst absent when the filesystem cannot clone, so the
// caller copies the bytes instead.
func cloneFile(src, dst string, perm os.FileMode) error {
	temporary, err := temporarySibling(dst)
	if err != nil {
		return err
	}
	if err := unix.Clonefile(src, temporary, unix.CLONE_NOFOLLOW); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Chmod(temporary, perm); err != nil {
		return errors.Join(err, os.Remove(temporary))
	}
	if err := os.Rename(temporary, dst); err != nil {
		return errors.Join(err, os.Remove(temporary))
	}
	return nil
}
