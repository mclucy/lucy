package log

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func DefaultFile() string {
	var directory string
	switch runtime.GOOS {
	case "windows":
		directory = filepath.Join(os.Getenv("APPDATA"), "lucy", "logs")
	case "darwin":
		directory = filepath.Join(os.Getenv("HOME"), "Library", "Logs", "lucy")
	case "linux":
		directory = filepath.Join(os.Getenv("HOME"), ".local", "share", "lucy", "logs")
	default:
		directory = "logs"
	}
	return filepath.Join(directory, "lucy.log")
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
