package snapshot

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fileidentity"
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
	// Matching entries carry the observed permission bits.
	Matching []Entry
	// Differing entries name a path that holds other content: other bytes or
	// a non-regular file.
	Differing []Entry
	// OtherPermissions entries name a managed regular file with the entry's
	// digest but other read or execute bits. They carry the observed
	// permission bits.
	OtherPermissions []Entry
	// Missing entries name an absent path.
	Missing []Entry
}

// Complete reports whether every entry matches.
func (c Comparison) Complete() bool {
	return len(c.Differing) == 0 && len(c.OtherPermissions) == 0 && len(c.Missing) == 0
}

// Compare digests each entry's path below dir. It refuses a path that leaves
// dir or traverses a symlink, so a hostile tree cannot redirect the read.
func Compare(dir string, entries []Entry) (Comparison, error) {
	return CompareCached(dir, entries, nil)
}

// compareResult is one entry's outcome. Observations are kept so a caller can
// record the evidence without a second walk.
type compareResult struct {
	entry            Entry
	differing        bool
	otherPermissions bool
	missing          bool
	err              error
}

// CompareCached digests each entry's path below dir, reusing a digest the memo
// already proved for an unchanged file. Files hash on a bounded pool; entry
// order decides the first error and the result lists.
func CompareCached(
	dir string,
	entries []Entry,
	memo *fileidentity.Memo,
) (Comparison, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return Comparison{}, err
	}
	defer root.Close()

	results := make([]compareResult, len(entries))
	workers := min(len(entries), 4)
	if workers < 1 {
		workers = 1
	}
	var next atomic.Int64
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			for {
				index := int(next.Add(1)) - 1
				if index >= len(entries) {
					return
				}
				results[index] = compareEntry(root, entries[index], memo)
			}
		})
	}
	group.Wait()

	var comparison Comparison
	for _, result := range results {
		switch {
		case result.err != nil:
			return Comparison{}, result.err
		case result.missing:
			comparison.Missing = append(comparison.Missing, result.entry)
		case result.differing:
			comparison.Differing = append(comparison.Differing, result.entry)
		case result.otherPermissions:
			comparison.OtherPermissions = append(comparison.OtherPermissions, result.entry)
		default:
			comparison.Matching = append(comparison.Matching, result.entry)
		}
	}
	return comparison, nil
}

func compareEntry(
	root *os.Root,
	entry Entry,
	memo *fileidentity.Memo,
) compareResult {
	result := compareResult{entry: entry}
	rel := filepath.FromSlash(entry.Path)
	if err := document.SafeRelativePath(rel); err != nil {
		result.err = fmt.Errorf("snapshot entry %s: %w", entry.Path, err)
		return result
	}
	info, err := lstatWithin(root, rel)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		result.missing = true
		return result
	case err != nil:
		result.err = err
		return result
	}
	if !info.Mode().IsRegular() {
		result.differing = true
		return result
	}
	observation, err := memo.ObserveRootInfo(root, rel, info, nil)
	if err != nil {
		result.err = err
		return result
	}
	digest := observation.Hashes["sha256"]
	if !strings.EqualFold(digest, entry.SHA256) {
		result.differing = true
		return result
	}
	result.entry.Mode = uint32(info.Mode().Perm())
	result.entry.SHA256 = digest
	if entry.Policy == managedPolicy && entry.Mode != 0 &&
		info.Mode().Perm()&0o555 != os.FileMode(entry.Mode)&0o555 {
		result.otherPermissions = true
	}
	return result
}

// VerifyRestored classifies entries against a tree a verified restore
// materialized. It checks presence, regular-file type, and the managed mode,
// but never reads content the caller already verified.
func VerifyRestored(dir string, entries []Entry) (Comparison, error) {
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
		observed := entry
		observed.Mode = uint32(info.Mode().Perm())
		if entry.Policy == managedPolicy && entry.Mode != 0 &&
			info.Mode().Perm()&0o555 != os.FileMode(entry.Mode)&0o555 {
			comparison.OtherPermissions = append(comparison.OtherPermissions, observed)
			continue
		}
		comparison.Matching = append(comparison.Matching, observed)
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
