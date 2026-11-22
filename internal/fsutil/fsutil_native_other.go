//go:build !windows && !(darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package fsutil

import (
	"fmt"
	"os"
	"runtime"
)

func openLockFile(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
}

func lockFile(file *os.File, wait bool) error {
	return unsupportedNative("file locking")
}

func unlockFile(file *os.File) error {
	return unsupportedNative("file unlocking")
}

func renameRoot(root *os.Root, from, to string, replace bool) error {
	return unsupportedNative("durable rename")
}

// sameDevice has no portable implementation on this platform, so a publish
// always stages beside its destination instead.
func sameDevice(first, second string) bool {
	return false
}

func unsupportedNative(operation string) error {
	return fmt.Errorf("%s is not supported on %s", operation, runtime.GOOS)
}

func makePrivateDirectories(path string, missing []string) error {
	return unsupportedNative("durable directory creation")
}
