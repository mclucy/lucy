//go:build !unix && !darwin && !linux

package server

import "os"

// directoryOwnedBy fails closed where Unix ownership cannot secure IPC directories.
func directoryOwnedBy(_ os.FileInfo, _ int) bool {
	return false
}
