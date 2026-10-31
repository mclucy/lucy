package operation

import (
	"context"
	"errors"
	"os"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/workspacewrite"
)

// Sync converges the workspace to its manifest and lock. A missing manifest or
// lock is allowed only when the other document can drive the operation.
func (s *Service) Sync(ctx context.Context, root string, opts install.Options) (*install.Result, error) {
	d, old, err := s.syncDocuments(root)
	if err != nil {
		return nil, err
	}
	if err := pinWorkspaceDocuments(&opts, d, old); err != nil {
		return nil, err
	}
	return s.install.Sync(ctx, root, d, old, opts)
}

// syncDocuments reads the documents a converge operation starts from. It takes
// the writer lock and replays interrupted journals first, so the installation
// that re-locks the workspace never plans from pre-recovery state.
func (s *Service) syncDocuments(root string) (manifest *document.Manifest, locked *document.Lock, resultErr error) {
	writer, err := workspacewrite.Lock(root, s.logger)
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := filetrack.RecoverLocked(root, s.logger); err != nil {
		return nil, nil, err
	}
	manifest, err = document.ManifestFile(root).Read()
	if errors.Is(err, os.ErrNotExist) {
		manifest, err = nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	locked, err = document.LockFile(root).Read()
	if errors.Is(err, os.ErrNotExist) {
		locked, err = nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	return manifest, locked, nil
}
