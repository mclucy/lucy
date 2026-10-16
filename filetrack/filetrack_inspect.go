package filetrack

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/types"
)

// DeclaredFiles prefers the manifest declarations and falls back to the lock
// when only an installed workspace remains, so read-only commands also work
// without a manifest. A present but unreadable state file is an error, never an
// empty result set.
func DeclaredFiles(root string) ([]types.TrackedFile, error) {
	document, manifestErr := manifest.Read(root)
	switch {
	case manifestErr == nil:
		return document.Files, nil
	case !errors.Is(manifestErr, os.ErrNotExist):
		return nil, fmt.Errorf("read manifest: %w", manifestErr)
	}

	locked, lockErr := lockfile.Read(root)
	switch {
	case lockErr == nil:
		return locked.Files, nil
	case !errors.Is(lockErr, os.ErrNotExist):
		return nil, fmt.Errorf("read lockfile: %w", lockErr)
	}

	return nil, fmt.Errorf(
		"no %s or %s in this workspace; run lucy init to create a manifest",
		manifest.Filename,
		lockfile.Filename,
	)
}

// Inspect makes no state directories and never contacts a recovery source.
func Inspect(root string, declarations []types.TrackedFile) (statuses []Status, resultErr error) {
	if err := noPending(root); err != nil {
		return nil, err
	}
	declarations = slices.Clone(declarations)
	if err := input.NormalizeTrackedFiles(declarations); err != nil {
		return nil, err
	}
	files, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	statuses = make([]Status, 0, len(declarations))
	for _, file := range declarations {
		status := Status{Path: file.Path, Immutable: file.Immutable, Hash: file.Hash, RecoverURL: file.RecoverURL}
		if err := workspacewrite.SafePath(root, file.Path); err != nil {
			status.State, status.Error = "error", err.Error()
			statuses = append(statuses, status)
			continue
		}
		info, err := files.Lstat(file.Path)
		switch {
		case errors.Is(err, os.ErrNotExist):
			status.State = "missing"
		case err != nil:
			status.State, status.Error = "error", err.Error()
		case !info.Mode().IsRegular():
			status.State, status.Error = "error", "tracked path is not a regular file"
		case file.Hash == "":
			status.State = "presence-only"
		default:
			actual, err := observe(files, file.Path)
			if err != nil {
				status.State, status.Error = "error", err.Error()
			} else {
				status.ActualHash = actual.Hash
				status.State = "matching"
				if actual.Hash != file.Hash {
					status.State = "modified"
				}
			}
		}
		statuses = append(statuses, status)
	}
	return statuses, nil
}

// Provision requires shared workspace ownership and preserves every existing working file.
// Availability failures are diagnostics; unsafe state and incomplete transactions are errors.
func Provision(ctx context.Context, root string, declarations []types.TrackedFile, offline bool) (diagnostics []Diagnostic, resultErr error) {
	if err := noPending(root); err != nil {
		return nil, err
	}
	declarations = slices.Clone(declarations)
	if err := input.NormalizeTrackedFiles(declarations); err != nil {
		return nil, err
	}
	if len(declarations) == 0 {
		return []Diagnostic{}, nil
	}
	files, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, files.Close()) }()
	store, err := cas.New(filepath.Join(root, ".lucy"))
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, store.Close()) }()
	diagnostics = make([]Diagnostic, 0)
	for _, file := range declarations {
		if err := ctx.Err(); err != nil {
			return diagnostics, err
		}
		if err := workspacewrite.SafePath(root, file.Path); err != nil {
			return diagnostics, err
		}
		info, err := files.Lstat(file.Path)
		if err == nil {
			if !info.Mode().IsRegular() {
				return diagnostics, fmt.Errorf("tracked path %s is not a regular file", file.Path)
			}
			if file.Hash == "" {
				continue
			}
			actual, err := capture(root, files, store, file.Path)
			if err != nil {
				return diagnostics, err
			}
			if actual.Hash != file.Hash && file.Immutable {
				diagnostics = append(diagnostics, Diagnostic{Path: file.Path, Message: "immutable contents differ; use lucy file update to accept them or lucy file recover to restore the accepted revision"})
			}
			continue
		}
		if !errors.Is(err, os.ErrNotExist) {
			return diagnostics, err
		}
		if file.Hash == "" {
			diagnostics = append(diagnostics, Diagnostic{Path: file.Path, Message: "file is missing and has no accepted hash; distribute its contents or track an accepted revision"})
			continue
		}
		accepted, err := sourceImage(ctx, store, file, false, offline)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: file.Path, Message: err.Error() + "; distribute the accepted contents or configure a source serving that digest"})
			continue
		}
		if _, err := files.Lstat(file.Path); !errors.Is(err, os.ErrNotExist) {
			if err != nil {
				return diagnostics, err
			}
			diagnostics = append(diagnostics, Diagnostic{Path: file.Path, Message: "file appeared during recovery and was preserved"})
			continue
		}
		reader, err := imageReader(store, accepted)
		if err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: file.Path, Message: "cannot open accepted contents: " + err.Error()})
			continue
		}
		copyErr := workspacewrite.CopyMissingRoot(files, file.Path, reader, os.FileMode(accepted.Mode))
		if err := errors.Join(copyErr, reader.Close()); err != nil {
			diagnostics = append(diagnostics, Diagnostic{Path: file.Path, Message: "cannot provision accepted contents: " + err.Error()})
		}
	}
	return diagnostics, nil
}
