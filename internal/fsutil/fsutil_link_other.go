//go:build !darwin && !linux

package fsutil

import (
	"errors"
	"os"
)

// cloneAt reports that this platform has no clone operation, so the caller
// copies the bytes instead.
func cloneAt(src string, dir *os.File, name string, perm os.FileMode) error {
	return errors.ErrUnsupported
}
