//go:build unix || darwin || linux

package server

import (
	"os"
	"syscall"
)

// directoryOwnedBy checks filesystem ownership rather than trusting directory mode alone.
func directoryOwnedBy(info os.FileInfo, uid int) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && int64(stat.Uid) == int64(uid)
}
