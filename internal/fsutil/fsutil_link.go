package fsutil

import (
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
// LinkMode.Perm. LinkHardlink links only when src already has exactly perm and
// both paths are on one device; otherwise it falls back to LinkClone, because
// a chmod on a shared inode would change src. LinkClone falls back to a byte
// copy when the filesystem cannot clone.
func Materialize(src, dst string, perm os.FileMode, mode LinkMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if placeFile(src, dst, perm, mode) {
		return nil
	}
	return AtomicCopyFile(src, dst, perm)
}

// placeFile places src at dst through the configured link mode and then a
// clone, and reports whether either succeeded. The caller copies the bytes
// when both fail.
func placeFile(src, dst string, perm os.FileMode, mode LinkMode) bool {
	if mode == LinkHardlink {
		if err := linkFile(src, dst, perm); err == nil {
			return true
		}
	}
	return cloneFile(src, dst, perm) == nil
}

// linkFile hardlinks src at dst through a temporary sibling. A hardlink
// shares one inode, so it applies only while src already carries perm; the
// caller falls back to a clone otherwise.
func linkFile(src, dst string, perm os.FileMode) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("link source is not a regular file")
	}
	if info.Mode().Perm() != perm {
		return fmt.Errorf("link source permissions %v differ from %v", info.Mode().Perm(), perm)
	}
	if !sameDevice(src, filepath.Dir(dst)) {
		return errors.New("link source is on another device")
	}
	temporary, err := temporarySibling(dst)
	if err != nil {
		return err
	}
	if err := os.Link(src, temporary); err != nil {
		return err
	}
	if err := os.Rename(temporary, dst); err != nil {
		return errors.Join(err, os.Remove(temporary))
	}
	return nil
}

// temporarySibling reserves a unique name beside dst that is free for a link
// or a clone to create. Both operations refuse an existing name, and the
// reserved name sits on the destination filesystem so the rename that
// publishes it never crosses a filesystem.
func temporarySibling(dst string) (string, error) {
	file, err := os.CreateTemp(filepath.Dir(dst), "."+filepath.Base(dst)+".*")
	if err != nil {
		return "", err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		return "", errors.Join(err, os.Remove(name))
	}
	if err := os.Remove(name); err != nil {
		return "", err
	}
	return name, nil
}
