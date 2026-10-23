package operation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
)

// DeinitResult reports the Lucy state that deinitialization removed. Paths are
// relative to the server root and listed in removal order.
type DeinitResult struct {
	Removed []string
}

// Deinitialize removes every path that marks root as a Lucy workspace and
// leaves the server it describes in place. Installed server content, worlds,
// and configuration stay untouched. An interrupted installation or file
// transaction is recovered before state is removed, and the writer lock keeps
// another Lucy process from writing while the state disappears.
func Deinitialize(root string) (result *DeinitResult, resultErr error) {
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
	markerExisted := slices.Contains(present, fsutil.WorkspaceMarker)

	writer, err := workspacewrite.Lock(root)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := workspacewrite.RecoverInstall(root); err != nil {
		return nil, err
	}
	pending, err := filetrack.HasPendingTransaction(root)
	if err != nil {
		return nil, err
	}
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
		manifest.Filename,
		lockfile.Filename,
		manifest.LocalFilename,
	} {
		if !slices.Contains(present, name) {
			continue
		}
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			return result, err
		}
		result.Removed = append(result.Removed, name)
		log.Debug("workspace state removed", "path", name)
	}

	if err := clearWorkspaceState(root); err != nil {
		return result, err
	}
	if err := writer.Close(); err != nil {
		return result, err
	}
	if err := removeWorkspaceMarker(root); err != nil {
		return result, err
	}
	if markerExisted {
		result.Removed = append(result.Removed, fsutil.WorkspaceMarker)
	}
	log.Info("workspace deinitialized", "root", root, "removed", len(result.Removed))
	return result, nil
}

// workspaceStatePaths names every top-level path that marks a Lucy workspace.
func workspaceStatePaths() []string {
	return []string{
		manifest.Filename,
		lockfile.Filename,
		manifest.LocalFilename,
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

// clearWorkspaceState removes the contents of the state directory while the
// writer lock is held. The lock file stays until the lock is released.
func clearWorkspaceState(root string) (resultErr error) {
	directory, err := os.OpenRoot(filepath.Join(root, fsutil.WorkspaceMarker))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	opened, err := directory.Open(".")
	if err != nil {
		return err
	}
	entries, readErr := opened.ReadDir(-1)
	if closeErr := opened.Close(); readErr != nil || closeErr != nil {
		return errors.Join(readErr, closeErr)
	}
	for _, entry := range entries {
		if entry.Name() == workspacewrite.LockFileName {
			continue
		}
		if err := directory.RemoveAll(entry.Name()); err != nil {
			return err
		}
	}
	return nil
}

// removeWorkspaceMarker deletes the empty state directory after the writer
// lock is released.
func removeWorkspaceMarker(root string) error {
	stateDirectory := filepath.Join(root, fsutil.WorkspaceMarker)
	if err := os.Remove(filepath.Join(stateDirectory, workspacewrite.LockFileName)); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.Remove(stateDirectory); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
