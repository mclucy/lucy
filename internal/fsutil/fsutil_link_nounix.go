//go:build !unix

package fsutil

import (
	"errors"
	"os"
)

// linkAt reports that this platform links no files, so the caller clones or
// copies instead.
func linkAt(src string, dir *os.File, name string) error {
	return errors.ErrUnsupported
}

// hardlinkSharesInode reports whether LinkHardlink can deploy a destination
// that shares the stored inode on this platform.
const hardlinkSharesInode = false
