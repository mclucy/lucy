package install

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/bootstrap/snapshot"
	"github.com/mclucy/lucy/document"
)

// ErrRuntimeImageUnavailable reports that Lucy cannot produce the runtime
// image of a lock without the network or a Java runtime. Adoption then claims
// only the files the lock pins by digest.
var ErrRuntimeImageUnavailable = errors.New("runtime image unavailable")

// storedRuntimeImage returns the runtime image the stored snapshot of the
// recipe describes, without materializing it.
func storedRuntimeImage(
	ctx context.Context,
	locked *document.Lock,
) ([]bootstrap.Output, bool) {
	key, err := snapshot.Key(locked.Server)
	if err != nil {
		return nil, false
	}
	store, err := openSnapshotStore()
	if err != nil {
		return nil, false
	}
	defer store.Close()
	snap, ok, err := store.LookupSnapshot(ctx, key)
	if err != nil || !ok {
		return nil, false
	}
	return snapshotOutputs(snap, locked.Server), true
}

// RuntimeImage returns the runtime image of a locked server: the files its
// bootstrap method produces, with their digests, modes, policies, and roles,
// sorted by path. It reads that image from the stored snapshot of the recipe,
// or builds it in a private stage under root from artifacts the store already
// holds and stores the result. It never writes the server directory and never
// uses the network; a missing artifact or Java runtime returns an error that
// wraps ErrRuntimeImageUnavailable.
//
// The caller must hold the workspace writer lock. Stage-only material is
// absent from the result.
func (s *Service) RuntimeImage(
	ctx context.Context,
	root string,
	locked *document.Lock,
	opts Options,
) (image []bootstrap.Output, resultErr error) {
	if locked == nil {
		return nil, errors.New("cannot build a runtime image without a lock")
	}
	if err := locked.Server.Bootstrap.Validate(); err != nil {
		return nil, err
	}
	if err := document.SafeRelativePath(locked.Server.Directory); err != nil {
		return nil, err
	}
	// A stored snapshot already names every runtime file's digest and mode.
	// Adoption compares the workspace against those values, so restoring the
	// bytes here would only repeat a read the comparison performs.
	if image, ok := storedRuntimeImage(ctx, locked); ok {
		return image, nil
	}

	opts.ProvisionJava = nil
	check, err := s.selectJava(ctx, root, locked, opts)
	if err != nil {
		return nil, err
	}
	// A runtime that misses the lock's requirement can fail a patcher or a
	// processor in ways no error type names. Without it, a method that runs
	// Java reports the missing runtime, and a snapshot still restores.
	java := ""
	if check.Satisfied() {
		java = check.Executable()
	}
	stage, err := os.MkdirTemp(filepath.Join(root, ".lucy"), "stage-*")
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := os.RemoveAll(stage); err != nil {
			image = nil
			// The staging cause is kept as text only: a cleanup failure is
			// fatal even when that cause would permit pinned adoption.
			if resultErr != nil {
				resultErr = fmt.Errorf("remove runtime image stage: %w (after: %v)", err, resultErr)
			} else {
				resultErr = fmt.Errorf("remove runtime image stage: %w", err)
			}
		}
	}()

	store := &downloadStore{
		ctx:       ctx,
		offline:   true,
		storeOnly: true,
		logger:    s.logger,
		injected:  opts.Materialize,
	}
	// Staging attaches the verified digests without rewriting the caller's lock.
	resolved := *locked
	resolved.Server.Inputs = slices.Clone(locked.Server.Inputs)
	outputs, err := s.stageServerTree(ctx, stage, &resolved, java, store, opts)
	if errors.Is(err, errOfflineArtifactUnavailable) || errors.Is(err, bootstrap.ErrNoJava) {
		return nil, fmt.Errorf("stage locked runtime: %w: %w", ErrRuntimeImageUnavailable, err)
	}
	if err != nil {
		return nil, err
	}

	image = make([]bootstrap.Output, 0, len(outputs))
	for _, output := range outputs {
		if output.Policy != bootstrap.PolicyDiscard && output.Role != bootstrap.RoleBootstrapOnly {
			image = append(image, output)
		}
	}
	slices.SortFunc(image, func(a, b bootstrap.Output) int {
		return cmp.Compare(a.Path, b.Path)
	})
	return image, nil
}
