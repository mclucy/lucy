//go:build !unix && !darwin && !linux

package server

import "os"

// consoleOpenFlags returns native file flags where Unix special files do not apply.
func consoleOpenFlags() int { return os.O_CREATE | os.O_APPEND | os.O_WRONLY }

// singleConsoleLink keeps metadata checks portable; managed run users are rejected
// on these unsupported service platforms before any child starts.
func singleConsoleLink(_ os.FileInfo) bool { return true }
