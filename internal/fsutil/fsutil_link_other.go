//go:build !darwin && !linux

package fsutil

import (
	"errors"
	"os"
)

// cloneFile reports that this platform has no clone operation, so Materialize
// copies the bytes instead.
func cloneFile(src, dst string, perm os.FileMode) error {
	return errors.ErrUnsupported
}
