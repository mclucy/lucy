package install

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/artifactstore"
	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/bootstrap/snapshot"
	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
	serverpkg "github.com/mclucy/lucy/server"
)

// restoreBootstrapSnapshot restores a stored output set into a fresh stage
// and returns the snapshot it restored. It reports false without an error
// when no snapshot exists, so the caller stages the method from scratch.
func restoreBootstrapSnapshot(
	ctx context.Context,
	store snapshot.Store,
	key cas.Digest,
	stage string,
	server document.LockedServer,
	link fsutil.LinkMode,
	trace *Trace,
) (snapshot.Snapshot, bool, error) {
	snap, ok, err := store.LookupSnapshot(ctx, key)
	if err != nil {
		return snapshot.Snapshot{}, false, err
	}
	if !ok {
		return snapshot.Snapshot{}, false, nil
	}
	root, err := serverRootPath(stage, server)
	if err != nil {
		return snapshot.Snapshot{}, false, err
	}
	if err := requireEmptyDirectory(root); err != nil {
		return snapshot.Snapshot{}, false, err
	}
	trace.phaseStarted("bootstrap snapshot")
	if err := store.RestoreSnapshot(ctx, snap, root, link); err != nil {
		return snapshot.Snapshot{}, false, err
	}
	return snap, true, nil
}

// publishBootstrapSnapshot stores the checked staging output for reuse by any
// workspace with the same recipe. Material a runtime does not consume (discard
// policy and bootstrap-only tools) is never stored.
//
// The link mode is a machine setting outside the recipe key, so a snapshot
// records the mode of a writable deployment. A method writes every output with
// the owner write bit; only a hardlink placement removes it. Restoring the bit
// here lets each restore apply its own link mode.
func publishBootstrapSnapshot(
	ctx context.Context,
	store snapshot.Store,
	key cas.Digest,
	stage string,
	server document.LockedServer,
	outputs map[string]bootstrap.Output,
) error {
	root, err := serverRootPath(stage, server)
	if err != nil {
		return err
	}
	entries := make([]snapshot.Entry, 0, len(outputs))
	for _, output := range outputs {
		if output.Policy == bootstrap.PolicyDiscard ||
			output.Role == bootstrap.RoleBootstrapOnly {
			continue
		}
		entries = append(entries, snapshot.Entry{
			Path:     output.ServerPath,
			SHA256:   output.SHA256,
			Mode:     writableMode(output.Mode),
			Policy:   string(output.Policy),
			Role:     string(output.Role),
			Evidence: string(output.Evidence),
		})
	}
	snap := snapshot.Snapshot{
		Key:     key,
		Entries: entries,
	}
	return store.SaveSnapshot(ctx, snap, root)
}

// writableMode adds the owner write bit to a recorded mode. Zero stays zero,
// because it marks an unrecorded mode.
func writableMode(mode uint32) uint32 {
	if mode == 0 {
		return 0
	}
	return mode | 0o200
}

// openSnapshotStore opens the artifact store that owns the snapshot index.
func openSnapshotStore() (*artifactstore.Store, error) {
	store, err := artifactstore.Open()
	if err != nil {
		return nil, fmt.Errorf("open artifact store snapshots: %w", err)
	}
	return store, nil
}

// serverRootPath validates the locked server directory and returns the
// directory a bootstrap method writes inside the staging root. A handcrafted
// lock may omit the directory, which means the staging root itself.
func serverRootPath(stage string, server document.LockedServer) (string, error) {
	directory := cmp.Or(server.Directory, ".")
	if err := document.SafeRelativePath(directory); err != nil {
		return "", err
	}
	return filepath.Join(stage, filepath.FromSlash(directory)), nil
}

// requireEmptyDirectory refuses to restore into a directory that already holds
// content. A snapshot restore must never merge with unrelated staging state.
func requireEmptyDirectory(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("staging path %s is not a directory", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("staging path %s is not empty; refusing to restore a snapshot over it", path)
	}
	return nil
}

// coreArtifactPath returns the offline installer used to verify Forge outputs.
// Only an object whose contents match the lock may supply that evidence.
func coreArtifactPath(server document.LockedServer) string {
	if server.Bootstrap != serverpkg.BootstrapMethodForge {
		return ""
	}
	value := server.Artifact.Hashes["sha256"]
	digest, err := cas.ParseDigest("sha256:" + value)
	if err != nil {
		return ""
	}
	store, err := artifactstore.Open()
	if err != nil {
		return ""
	}
	defer store.Close()
	path := store.Path(digest)
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return ""
	}
	if _, err := DigestFile(path, server.Artifact.Hashes); err != nil {
		return ""
	}
	return path
}
