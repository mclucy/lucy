//go:build unix || darwin || linux

package cli

import (
	"os"
	"syscall"
)

// consoleOpenFlags avoids blocking on a FIFO before file metadata is inspected.
func consoleOpenFlags() int {
	return os.O_CREATE | os.O_APPEND | os.O_WRONLY | syscall.O_NONBLOCK
}

// singleConsoleLink rejects hard-linked targets before privileged console writes.
func singleConsoleLink(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Nlink == 1
}
