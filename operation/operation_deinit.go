package operation

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspacewrite"
)

// DeinitResult reports the Lucy state that deinitialization removed. Paths are
// relative to the server root and listed in removal order.
type DeinitResult struct {
	Removed  []string
	Warnings []error
}

// Deinitialize removes every path that marks root as a Lucy workspace and
// leaves the server it describes in place. Installed server content, worlds,
// and configuration stay untouched. An interrupted installation or file
// transaction is recovered before state is removed, and the writer lock keeps
// another Lucy process from writing while the state disappears.
func (s *Service) Deinitialize(root string) (result *DeinitResult, resultErr error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	present, err := presentWorkspacePaths(root)
	if err != nil {
		return nil, err
	}
	if len(present) == 0 {
		return nil, fmt.Errorf("no lucy workspace found in %s", root)
	}

	writer, err := workspacewrite.Lock(root, s.logger)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := workspacewrite.RecoverInstall(root, s.logger); err != nil {
		return nil, err
	}
	pending, err := filetrack.HasPendingTransaction(root)
	if err != nil {
		return nil, err
	}
	// State that only a pending transaction would touch is about to be
	// deleted, so a damaged history alone must not block deinitialization.
	if pending {
		if err := filetrack.RecoverPending(root); err != nil {
			return nil, err
		}
	}

	// Recovery can publish declarations that an interrupted transaction
	// restored, so observe the workspace again before removing anything.
	present, err = presentWorkspacePaths(root)
	if err != nil {
		return nil, err
	}
	result = &DeinitResult{}
	for _, name := range []string{
		document.ManifestFilename,
		document.LockFilename,
	} {
		if !slices.Contains(present, name) {
			continue
		}
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			return result, err
		}
		result.Removed = append(result.Removed, name)
		s.logger.Debug("workspace state removed", "path", name)
	}
	if s.registry == nil {
		result.Warnings = append(
			result.Warnings,
			errors.New(
				"server unregistration unavailable: no server registry was supplied; registered entries for this workspace may remain",
			),
		)
	} else if err := s.registry.Unregister(root); err != nil {
		result.Warnings = append(result.Warnings, fmt.Errorf("server unregistration failed: %w", err))
	}

	detached, err := detachWorkspaceState(root)
	if err != nil {
		return result, err
	}
	if err := writer.Close(); err != nil {
		return result, err
	}
	if err := removeDetachedState(root, detached); err != nil {
		return result, err
	}
	result.Removed = append(result.Removed, fsutil.WorkspaceMarker)
	s.logger.Info("workspace deinitialized", "root", root, "removed", len(result.Removed))
	return result, nil
}

// workspaceStatePaths names every top-level path that marks a Lucy workspace.
func workspaceStatePaths() []string {
	return []string{
		document.ManifestFilename,
		document.LockFilename,
		fsutil.WorkspaceMarker,
	}
}

// presentWorkspacePaths returns the state paths that exist in root. Symlinks
// and wrong file kinds fail instead of being removed, so a link cannot redirect
// deletion outside the workspace.
func presentWorkspacePaths(root string) ([]string, error) {
	present := make([]string, 0, len(workspaceStatePaths()))
	for _, name := range workspaceStatePaths() {
		if err := workspacewrite.SafePath(root, name); err != nil {
			return nil, err
		}
		info, err := os.Lstat(filepath.Join(root, name))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if name == fsutil.WorkspaceMarker {
			if !info.IsDir() {
				return nil, fmt.Errorf("%s must be a directory", name)
			}
		} else if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("%s must be a regular file", name)
		}
		present = append(present, name)
	}
	return present, nil
}

// detachWorkspaceState moves the state directory aside while the writer lock
// is held. A writer that acquires the lock after this rename creates a new
// directory and a new lock file, so removing the detached one cannot pull the
// inode out from under it.
func detachWorkspaceState(root string) (string, error) {
	detached := fsutil.WorkspaceMarker + "-removed-" + rand.Text()
	if err := os.Rename(
		filepath.Join(root, fsutil.WorkspaceMarker),
		filepath.Join(root, detached),
	); err != nil {
		return "", err
	}
	return detached, nil
}

// removeDetachedState deletes the detached state directory after the writer
// lock is released. Root-confined removal keeps a symlink inside it from
// redirecting the deletion.
func removeDetachedState(root, detached string) (resultErr error) {
	files, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	return files.RemoveAll(detached)
}
