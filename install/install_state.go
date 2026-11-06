package install

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/receipt"
)

// installationCurrent reports whether the receipt answers the lock and every
// recorded output still holds its committed bytes.
func installationCurrent(root string, l *document.Lock, logger *log.Logger) (bool, error) {
	current, err := receipt.Read(root)
	if errors.Is(err, os.ErrNotExist) {
		logger.Debug("installation not current", "reason", "no install receipt")
		return false, nil
	}
	if err != nil {
		return false, err
	}
	expected, err := receipt.ResolutionFingerprint(l)
	if err != nil {
		return false, err
	}
	if current.LockFingerprint != expected {
		logger.Debug(
			"installation not current",
			"reason", "lock fingerprint changed",
			"receipt", current.LockFingerprint,
			"expected", expected,
		)
		return false, nil
	}
	tracked := trackedPaths(l)
	for _, entry := range current.Entries {
		if tracked[strings.ToLower(entry.Path)] {
			continue
		}
		if err := workspacewrite.SafePath(root, entry.Path); err != nil {
			return false, err
		}
		actual, err := DigestFile(filepath.Join(root, entry.Path), nil)
		if os.IsNotExist(err) {
			logger.Debug("installation not current", "reason", "owned output missing", "path", entry.Path)
			return false, nil
		}
		if err != nil {
			return false, err
		}
		if actual["sha256"] != entry.SHA256 {
			logger.Debug("installation not current", "reason", "owned output modified", "path", entry.Path)
			return false, nil
		}
	}
	logger.Debug(
		"installation current",
		"reason", "receipt matches lock and every owned output",
		"receipt", filepath.Join(root, receipt.Filename),
	)
	return true, nil
}

// trackedPaths lowercases the lock's tracked-file paths, because the
// filesystems that host a workspace may compare them case-insensitively.
func trackedPaths(l *document.Lock) map[string]bool {
	paths := make(map[string]bool, len(l.Files))
	for _, file := range l.Files {
		paths[strings.ToLower(file.Path)] = true
	}
	return paths
}
