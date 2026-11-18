package log

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// DefaultDirectory names the per-user directory that holds Lucy's diagnostic
// records. Every retained diagnostic file derives from it, so their
// locations cannot drift apart.
func DefaultDirectory() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "lucy", "logs")
	case "darwin":
		return filepath.Join(os.Getenv("HOME"), "Library", "Logs", "lucy")
	case "linux":
		return filepath.Join(os.Getenv("HOME"), ".local", "share", "lucy", "logs")
	default:
		return "logs"
	}
}

func DefaultFile() string {
	return filepath.Join(DefaultDirectory(), "lucy.log")
}

func openLogFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open log file %q: %w", path, err)
	}
	return file, nil
}
