//go:build !unix && !darwin && !linux

package server

// WithInstanceLock runs fn without cross-process exclusion on platforms where
// Lucy has no native file-lock implementation.
func WithInstanceLock(_ string, fn func() error) error {
	return fn()
}
