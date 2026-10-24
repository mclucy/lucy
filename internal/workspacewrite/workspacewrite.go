// Package workspacewrite coordinates recoverable filesystem writes without runtime ownership policy.
package workspacewrite

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/log"
	"gopkg.in/yaml.v3"
)

const (
	InstallJournalName = ".lucy/commit.yaml"

	// LockFileName names the writer lock inside the workspace state directory.
	LockFileName = "writer.lock"
)

// InstallJournal preserves the existing installation rollback record format.
type InstallJournal struct {
	Backup  string   `yaml:"backup"`
	Changed []string `yaml:"changed"`
	Created []string `yaml:"created"`
}

// Lock is shared by installation and explicit file operations. It reclaims
// staging that a killed publisher abandoned, which is safe only under the
// writer lock it takes, because that lock proves no publisher is live.
func Lock(root string) (*fsutil.FileLock, error) {
	if err := SafePath(root, ".lucy"); err != nil {
		return nil, err
	}
	if err := fsutil.MkdirAllPrivate(filepath.Join(root, ".lucy")); err != nil {
		return nil, err
	}
	if err := SafePath(root, ".lucy/"+LockFileName); err != nil {
		return nil, err
	}
	lock, err := fsutil.TryLockFile(filepath.Join(root, ".lucy", LockFileName))
	if err != nil {
		if errors.Is(err, fsutil.ErrLocked) {
			return nil, fmt.Errorf("another Lucy operation owns this workspace: %w", err)
		}
		return nil, err
	}
	files, err := os.OpenRoot(root)
	if err != nil {
		return nil, errors.Join(err, lock.Close())
	}
	removed, sweepErr := fsutil.SweepStaging(files)
	closeErr := files.Close()
	if sweepErr != nil {
		log.Warn(fmt.Errorf("reclaim stale staging files: %w", sweepErr))
	}
	for _, name := range removed {
		log.Debug("removed stale staging file", "path", name)
	}
	if closeErr != nil {
		return nil, errors.Join(closeErr, lock.Close())
	}
	return lock, nil
}

// SafePath rejects existing symlink components before root-confined IO.
func SafePath(root, relative string) error {
	if !filepath.IsLocal(relative) {
		return fmt.Errorf("output path must stay inside the workspace")
	}
	current := root
	for component := range strings.SplitSeq(filepath.Clean(relative), string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("output path traverses symlink: %s", relative)
		}
	}
	return nil
}

// RemoveRoot persists disappearance from its original name; leftovers are private cleanup data.
func RemoveRoot(root *os.Root, relative string) error {
	if !filepath.IsLocal(relative) {
		return fmt.Errorf("output path must stay inside the workspace")
	}
	if _, err := root.Lstat(relative); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if err := root.MkdirAll(".lucy/tmp", 0o700); err != nil {
		return err
	}
	garbage := ".lucy/tmp/removed-" + rand.Text()
	if err := fsutil.RenameRoot(root, relative, garbage); err != nil {
		return err
	}
	if err := root.Chmod(garbage, 0o600); err != nil {
		return err
	}
	return root.Remove(garbage)
}

// RecoverInstall must run with workspace ownership before reading state for a new mutation.
func RecoverInstall(root string) (resultErr error) {
	if err := SafePath(root, InstallJournalName); err != nil {
		return err
	}
	raw, err := os.ReadFile(filepath.Join(root, InstallJournalName))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := CheckInactive(root, "recovering an interrupted installation"); err != nil {
		return err
	}
	var journal InstallJournal
	if err := yaml.Unmarshal(raw, &journal); err != nil {
		return err
	}
	relativeBackup, err := filepath.Rel(filepath.Join(root, ".lucy"), journal.Backup)
	if err != nil || !strings.HasPrefix(relativeBackup, "rollback-") || strings.ContainsAny(relativeBackup, "/\\") {
		return fmt.Errorf("invalid installation rollback directory")
	}
	if err := SafePath(root, filepath.Join(".lucy", relativeBackup)); err != nil {
		return err
	}
	journalPath := filepath.Join(root, InstallJournalName)
	log.Warn(
		fmt.Errorf(
			"recovering interrupted installation transaction %s: %d created outputs to remove, %d changed outputs to restore",
			journalPath, len(journal.Created), len(journal.Changed),
		),
	)
	files, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	for _, name := range journal.Created {
		if err := SafePath(root, name); err != nil {
			return err
		}
		if err := RemoveRoot(files, name); err != nil {
			return err
		}
	}
	for _, name := range journal.Changed {
		if err := SafePath(root, name); err != nil {
			return err
		}
		source, err := files.Open(filepath.Join(".lucy", relativeBackup, name))
		if err != nil {
			return fmt.Errorf("open installation pre-image %s: %w", name, err)
		}
		info, statErr := source.Stat()
		if statErr != nil {
			return errors.Join(statErr, source.Close())
		}
		copyErr := fsutil.WriteRoot(files, name, source, fsutil.WriteOptions{
			Mode:    info.Mode().Perm(),
			Replace: true,
		})
		if closeErr := source.Close(); copyErr != nil || closeErr != nil {
			return errors.Join(copyErr, closeErr)
		}
	}
	if err := RemoveRoot(files, InstallJournalName); err != nil {
		return err
	}
	if err := files.RemoveAll(filepath.Join(".lucy", relativeBackup)); err != nil {
		return err
	}
	log.Info(
		"installation transaction recovered",
		"root", root,
		"journal", journalPath,
	)
	return nil
}
