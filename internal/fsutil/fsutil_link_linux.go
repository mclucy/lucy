//go:build linux

package fsutil

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// cloneFile places a copy-on-write clone of src at dst with perm. It reports
// an error and leaves dst absent when the filesystem cannot clone, so the
// caller copies the bytes instead.
func cloneFile(src, dst string, perm os.FileMode) error {
	source, err := os.Open(src)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*")
	if err != nil {
		return err
	}
	temporary := target.Name()
	discard := func(err error) error {
		return errors.Join(err, target.Close(), os.Remove(temporary))
	}
	if err := unix.IoctlFileClone(int(target.Fd()), int(source.Fd())); err != nil {
		return discard(err)
	}
	if err := target.Chmod(perm); err != nil {
		return discard(err)
	}
	if err := target.Close(); err != nil {
		return errors.Join(err, os.Remove(temporary))
	}
	if err := os.Rename(temporary, dst); err != nil {
		return errors.Join(err, os.Remove(temporary))
	}
	return nil
}
