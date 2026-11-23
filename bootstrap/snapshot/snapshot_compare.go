package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/document"
)

// managedPolicy is the entry policy whose permission bits belong to its
// identity.
const managedPolicy = "managed"

// Comparison classifies snapshot entries by the state of their path below a
// directory. Every entry lands in exactly one list, in entry order. Files in
// the directory that no entry names are outside the comparison.
type Comparison struct {
	// Matching entries name a regular file with the entry's digest. A managed
	// entry also compares the read and execute bits of its mode, because a
	// read-only copy or hard link runs the same as a writable one. A seed file
	// belongs to the user once it exists, so its mode is never compared.
	Matching []Entry
	// Differing entries name a path that holds other content: other bytes, a
	// managed file with other permission bits, or a non-regular file.
	Differing []Entry
	// Missing entries name an absent path.
	Missing []Entry
}

// Complete reports whether every entry matches.
func (c Comparison) Complete() bool {
	return len(c.Differing) == 0 && len(c.Missing) == 0
}

// Compare digests each entry's path below dir. It refuses a path that leaves
// dir or traverses a symlink, so a hostile tree cannot redirect the read.
func Compare(dir string, entries []Entry) (Comparison, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Comparison{}, err
	}
	defer root.Close()

	var comparison Comparison
	for _, entry := range entries {
		rel := filepath.FromSlash(entry.Path)
		if err := document.SafeRelativePath(rel); err != nil {
			return Comparison{}, fmt.Errorf("snapshot entry %s: %w", entry.Path, err)
		}
		info, err := lstatWithin(root, rel)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			comparison.Missing = append(comparison.Missing, entry)
			continue
		case err != nil:
			return Comparison{}, err
		}
		if !info.Mode().IsRegular() {
			comparison.Differing = append(comparison.Differing, entry)
			continue
		}
		digest, err := digestWithin(root, rel)
		if err != nil {
			return Comparison{}, err
		}
		if !strings.EqualFold(digest, entry.SHA256) {
			comparison.Differing = append(comparison.Differing, entry)
			continue
		}
		if entry.Policy == managedPolicy && entry.Mode != 0 &&
			info.Mode().Perm()&0o555 != os.FileMode(entry.Mode)&0o555 {
			comparison.Differing = append(comparison.Differing, entry)
			continue
		}
		comparison.Matching = append(comparison.Matching, entry)
	}
	return comparison, nil
}

// lstatWithin reports the entry at rel below root without following a symlink
// in any component. A symlink above the leaf is an error, and a leaf symlink
// is returned as a non-regular file.
func lstatWithin(root *os.Root, rel string) (fs.FileInfo, error) {
	parts := strings.Split(filepath.ToSlash(rel), "/")
	current := ""
	for i, part := range parts {
		if part == "" || part == "." {
			continue
		}
		current = filepath.Join(current, part)
		info, err := root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if i == len(parts)-1 {
			return info, nil
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return nil, fmt.Errorf("snapshot path %s traverses symlink %s", rel, current)
		}
		if !info.IsDir() {
			return nil, fs.ErrNotExist
		}
	}
	return nil, fs.ErrNotExist
}

// digestWithin returns the lowercase hexadecimal SHA-256 digest of the file at
// rel below root.
func digestWithin(root *os.Root, rel string) (string, error) {
	file, err := root.Open(rel)
	if err != nil {
		return "", err
	}
	hasher := sha256.New()
	_, copyErr := io.Copy(hasher, file)
	closeErr := file.Close()
	if copyErr != nil || closeErr != nil {
		return "", errors.Join(copyErr, closeErr)
	}
	return hex.EncodeToString(hasher.Sum(nil)), nil
}
