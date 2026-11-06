// Package appdata locates the per-user directory Lucy owns for durable local
// state. It is the single authority for the platform's local application data
// conventions; packages that need durable state append their own subdirectory
// to the root instead of repeating the platform policy.
package appdata

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// directoryName is the directory Lucy owns under the platform's local
// application data base.
const directoryName = "lucy"

// Root reports the per-user directory Lucy owns for durable machine state:
// %LOCALAPPDATA%\lucy on Windows, ~/Library/Application Support/lucy on macOS,
// and $XDG_DATA_HOME/lucy or ~/.local/share/lucy elsewhere. A relative
// XDG_DATA_HOME is ignored in favor of the home fallback, as the XDG base
// directory specification requires. Root never creates the directory.
func Root() (string, error) {
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("locate lucy data directory: LOCALAPPDATA is not set")
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate lucy data directory: %w", err)
		}
		base = filepath.Join(home, "Library", "Application Support")
	default:
		base = os.Getenv("XDG_DATA_HOME")
		if !filepath.IsAbs(base) {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("locate lucy data directory: %w", err)
			}
			base = filepath.Join(home, ".local", "share")
		}
	}
	return filepath.Join(base, directoryName), nil
}
