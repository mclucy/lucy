package server

import (
	"errors"
	"fmt"
	"os"

	"github.com/mclucy/lucy/internal/fsutil"
)

// readOptionalFile reads a file, treating a missing file as a non-error.
func readOptionalFile(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, true, nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("read %s: %w", path, err)
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	return fsutil.AtomicWrite(path, data, mode)
}
