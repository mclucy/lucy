//go:build !unix

package fsutil

import "testing"

// This platform cannot link a stored inode, so hardlink deploys a plain copy
// that keeps the requested write bits.
func TestLinkModePermWithoutLinking(t *testing.T) {
	if got := LinkHardlink.Perm(0o644); got != 0o644 {
		t.Fatalf("hardlink perm = %#o, want %#o", got, 0o644)
	}
}
