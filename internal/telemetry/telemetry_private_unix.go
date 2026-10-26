//go:build !windows

package telemetry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func checkPrivateDirectory(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect telemetry directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("telemetry path %q is not a directory", path)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("telemetry directory %q is not private", path)
	}
	return nil
}

func checkPrivateFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("inspect telemetry file: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("telemetry file %q is not private", path)
	}
	return nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create telemetry directory: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("restrict telemetry directory: %w", err)
	}
	return checkPrivateDirectory(path)
}

func writePrivateFile(path string, content []byte) error {
	directory := filepath.Dir(path)
	if err := ensurePrivateDirectory(directory); err != nil {
		return err
	}
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, content, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return nil
}
