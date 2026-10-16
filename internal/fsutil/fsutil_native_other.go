//go:build !windows && !(darwin || dragonfly || freebsd || linux || netbsd || openbsd || solaris)

package fsutil

import (
	"fmt"
	"os"
	"runtime"
)

func lockFile(file *os.File, wait bool) error {
	return unsupportedNative("file locking")
}

func unlockFile(file *os.File) error {
	return unsupportedNative("file unlocking")
}

func renameRoot(root *os.Root, from, to string) error {
	return unsupportedNative("durable rename")
}

func unsupportedNative(operation string) error {
	return fmt.Errorf("%s is not supported on %s", operation, runtime.GOOS)
}

func makePrivateDirectories(path string, missing []string) error {
	return unsupportedNative("durable directory creation")
}
