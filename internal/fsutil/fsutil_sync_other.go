//go:build !darwin

package fsutil

import "os"

// Barrier persists the writes to file before it returns. Platforms other
// than darwin have no cheaper ordering-only primitive.
func Barrier(file *os.File) error {
	return file.Sync()
}

// Flush has no work because every Barrier is already durable.
func Flush(file *os.File) error {
	return nil
}
