//go:build unix

package fsutil

import "testing"

// A hardlink shares the stored inode on this platform, so the destination must
// stay read-only and a clone must keep the requested mode.
func TestLinkModePermOnUnix(t *testing.T) {
	if got := LinkHardlink.Perm(0o644); got != 0o444 {
		t.Fatalf("hardlink perm = %#o, want %#o", got, 0o444)
	}
	if got := LinkClone.Perm(0o644); got != 0o644 {
		t.Fatalf("clone perm = %#o, want %#o", got, 0o644)
	}
}
