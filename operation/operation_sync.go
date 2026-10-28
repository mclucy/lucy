package operation

import (
	"context"
	"errors"
	"os"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/install"
)

// Sync converges the workspace to its manifest and lock. A missing manifest or
// lock is allowed only when the other document can drive the operation.
func (s *Service) Sync(ctx context.Context, root string, opts install.Options) (*install.Result, error) {
	d, err := document.ManifestFile(root).Read()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if errors.Is(err, os.ErrNotExist) {
		d = nil
	}
	old, err := document.LockFile(root).Read()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if errors.Is(err, os.ErrNotExist) {
		old = nil
	}
	return s.install.Sync(ctx, root, d, old, opts)
}
