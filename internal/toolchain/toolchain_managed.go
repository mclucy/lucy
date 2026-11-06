package toolchain

import (
	"path/filepath"

	"github.com/mclucy/lucy/internal/appdata"
)

// ManagedJavaRoot returns the per-user directory that holds the Java runtimes
// Lucy downloads. Each runtime is one child directory of the root.
func ManagedJavaRoot() (string, error) {
	root, err := appdata.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "java"), nil
}
