package toolchain

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// ManagedJavaRoot returns the per-user directory that holds the Java runtimes
// Lucy downloads. Each runtime is one child directory of the root.
func ManagedJavaRoot() (string, error) {
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("LOCALAPPDATA is not set")
		}
		return filepath.Join(base, "lucy", "java"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "lucy", "java"), nil
	default:
		if data := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(data) {
			return filepath.Join(data, "lucy", "java"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".local", "share", "lucy", "java"), nil
	}
}
