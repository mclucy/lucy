package operation

import (
	"context"
	"errors"
	"os"

	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
)

// Sync converges the workspace to its manifest and lock. A missing manifest or
// lock is allowed only when the other document can drive the operation.
func Sync(ctx context.Context, root string, opts install.Options) (*install.Result, error) {
	d, err := manifest.Read(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if errors.Is(err, os.ErrNotExist) {
		d = nil
	}
	old, err := lockfile.Read(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if errors.Is(err, os.ErrNotExist) {
		old = nil
	}
	observeManifest(d)
	observeLock(old)
	return install.Sync(ctx, root, d, old, opts)
}
