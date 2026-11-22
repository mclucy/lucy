package fsutil

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// LinkMode selects how stored content reaches a destination.
type LinkMode string

const (
	// LinkClone shares the stored blocks through a copy-on-write clone where
	// the filesystem supports it and copies the bytes elsewhere. A later write
	// to the destination never reaches the stored content.
	LinkClone LinkMode = "clone"
	// LinkHardlink shares the stored inode. A process that writes the
	// destination in place also changes the stored content, so this mode is
	// opt-in and deploys read-only files.
	LinkHardlink LinkMode = "hardlink"
)

// ParseLinkMode reads a configured link mode. An empty value selects
// LinkClone.
func ParseLinkMode(value string) (LinkMode, error) {
	switch LinkMode(value) {
	case "", LinkClone:
		return LinkClone, nil
	case LinkHardlink:
		return LinkHardlink, nil
	default:
		return "", fmt.Errorf("unknown link mode %q; use %q or %q", value, LinkClone, LinkHardlink)
	}
}

// Perm reports the permission set a destination requests under this mode. A
// hardlink shares the store inode, so the destination is read-only; a clone or
// a copy keeps the requested permissions.
func (m LinkMode) Perm(requested os.FileMode) os.FileMode {
	if m == LinkHardlink {
		return requested &^ 0o222
	}
	return requested
}

// Materialize places the content of src at dst with permission bits perm and
// replaces dst atomically. src is never modified, and an interrupted call
// never leaves a partial dst.
//
// A hardlink deploys a read-only destination, so callers compute perm through
// LinkMode.Perm. LinkHardlink links only when src already has exactly perm,
// because a chmod on a shared inode would change src; otherwise it falls back
// to a clone. A clone falls back to a byte copy.
func Materialize(src, dst string, perm os.FileMode, mode LinkMode) error {
	parent := filepath.Dir(dst)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return err
	}
	dir, err := os.Open(parent)
	if err != nil {
		return err
	}
	defer dir.Close()
	temporary := "." + filepath.Base(dst) + "." + rand.Text()
	if !placeFileAt(dir, temporary, src, perm, mode) {
		return AtomicCopyFile(src, dst, perm)
	}
	if err := os.Rename(filepath.Join(parent, temporary), dst); err != nil {
		return errors.Join(err, os.Remove(filepath.Join(parent, temporary)))
	}
	return nil
}

// placeFileAt creates name in dir with the content of src through a hardlink
// or a clone and reports whether one succeeded. name must be absent, and a
// failed attempt leaves it absent, so the caller copies the bytes instead.
// Each operation resolves name relative to dir, so a path change above dir
// cannot redirect the write.
func placeFileAt(dir *os.File, name, src string, perm os.FileMode, mode LinkMode) bool {
	if mode == LinkHardlink && linkSourceHasPerm(src, perm) && linkAt(src, dir, name) == nil {
		return true
	}
	return cloneAt(src, dir, name, perm) == nil
}

// linkSourceHasPerm reports whether src is a regular file that already
// carries perm. A hardlink applies only then, because the destination shares
// the source inode and its mode.
func linkSourceHasPerm(src string, perm os.FileMode) bool {
	info, err := os.Lstat(src)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm() == perm
}
