//go:build !unix && !darwin && !linux

package server

func WithInstanceLock(_ string, fn func() error) error {
	return fn()
}
