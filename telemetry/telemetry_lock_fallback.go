//go:build !linux && !darwin && !freebsd && !netbsd && !openbsd && !dragonfly && !windows

package telemetry

import "os"

// Platforms without a supported advisory file lock still get in-process
// serialization; cross-process coordination degrades to atomic replacement.
func tryFileLock(_ *os.File) (bool, error) {
	return true, nil
}

func unlockFile(_ *os.File) {}
