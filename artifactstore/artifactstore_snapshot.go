package artifactstore

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/bootstrap/snapshot"
	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/internal/fsutil"
)

const (
	snapshotsDirectoryName = "snapshots"
	snapshotManifestSuffix = ".json"
	// snapshotManifestLimit bounds a manifest read so a damaged file cannot
	// exhaust memory.
	snapshotManifestLimit = 32 * 1024 * 1024
)

// LookupSnapshot reads the manifest for key. A missing or unreadable manifest
// reports a miss; the caller then stages the bootstrap from scratch.
func (s *Store) LookupSnapshot(ctx context.Context, key cas.Digest) (snapshot.Snapshot, bool, error) {
	if err := ctx.Err(); err != nil {
		return snapshot.Snapshot{}, false, err
	}
	raw, ok, err := s.readSnapshotManifest(key)
	if err != nil || !ok {
		return snapshot.Snapshot{}, ok, err
	}
	var snap snapshot.Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return snapshot.Snapshot{}, false, fmt.Errorf("decode bootstrap snapshot %s: %w", key.Hex(), err)
	}
	if snap.Key != key {
		return snapshot.Snapshot{}, false, fmt.Errorf(
			"bootstrap snapshot %s names a different key %s",
			key.Hex(), snap.Key.Hex(),
		)
	}
	if err := snap.Validate(); err != nil {
		return snapshot.Snapshot{}, false, fmt.Errorf("invalid bootstrap snapshot %s: %w", key.Hex(), err)
	}
	return snap, true, nil
}

// SaveSnapshot publishes every entry's contents to the store, then writes the
// manifest. A failure before the manifest write leaves the published objects
// unreferenced.
func (s *Store) SaveSnapshot(ctx context.Context, snap snapshot.Snapshot, sourceDir string) error {
	if err := snap.Validate(); err != nil {
		return err
	}
	snap.Version = snapshot.FormatVersion
	snap.Entries = slices.Clone(snap.Entries)
	slices.SortFunc(snap.Entries, func(a, b snapshot.Entry) int {
		return strings.Compare(a.Path, b.Path)
	})
	for _, entry := range snap.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		digest, err := cas.ParseDigest("sha256:" + strings.ToLower(entry.SHA256))
		if err != nil {
			return fmt.Errorf("snapshot entry %s: %w", entry.Path, err)
		}
		source := filepath.Join(sourceDir, filepath.FromSlash(entry.Path))
		if err := s.storeObject(digest, source); err != nil {
			return fmt.Errorf("snapshot entry %s: %w", entry.Path, err)
		}
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("encode bootstrap snapshot: %w", err)
	}
	return s.writeSnapshotManifest(snap.Key, raw)
}

// storeObject publishes source at the digest address unless the object is
// already present. Contents are addressed by digest, so a present object needs
// no second read; a corrupt object is replaced when a later writer publishes
// verified bytes at the same address.
func (s *Store) storeObject(digest cas.Digest, source string) error {
	if info, err := os.Lstat(s.Path(digest)); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("store object %s is not a regular file", digest.Hex())
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	file, err := os.Open(source)
	if err != nil {
		return err
	}
	defer file.Close()
	if _, err := s.Store.Put(file, &digest); err != nil {
		return err
	}
	return nil
}

// RestoreSnapshot materializes every entry into destination. Bytes are
// verified against their digest while copying. The caller owns destination;
// a failed restore leaves it incomplete, and the caller removes it before a
// cold start.
func (s *Store) RestoreSnapshot(ctx context.Context, snap snapshot.Snapshot, destination string) error {
	if err := snap.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return fmt.Errorf("create snapshot destination: %w", err)
	}
	var recoverable error
	for _, entry := range snap.Entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		digest, err := cas.ParseDigest("sha256:" + strings.ToLower(entry.SHA256))
		if err != nil {
			return fmt.Errorf("snapshot entry %s: %w", entry.Path, err)
		}
		if err := s.restoreEntry(digest, entry, destination); err != nil {
			failure := fmt.Errorf("restore %s: %w", entry.Path, err)
			if errors.Is(err, cas.ErrCorrupt) {
				// Drop every corrupt object in this restore so one cold
				// prepare can republish all of them from checked output.
				if _, removeErr := s.Remove(digest); removeErr != nil {
					return errors.Join(recoverable, failure, removeErr)
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				return errors.Join(recoverable, failure)
			}
			recoverable = errors.Join(recoverable, failure)
		}
	}
	return recoverable
}

func (s *Store) restoreEntry(digest cas.Digest, entry snapshot.Entry, destination string) error {
	reader, err := s.Open(digest)
	if err != nil {
		return err
	}
	target := filepath.Join(destination, filepath.FromSlash(entry.Path))
	mode := os.FileMode(entry.Mode)
	if mode == 0 {
		mode = 0o644
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return errors.Join(err, reader.Close())
	}
	out, err := os.CreateTemp(filepath.Dir(target), ".restore-*")
	if err != nil {
		return errors.Join(err, reader.Close())
	}
	temporary := out.Name()
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(temporary)
		}
	}()
	if _, err := io.Copy(out, reader); err != nil {
		return errors.Join(err, out.Close(), reader.Close())
	}
	if err := reader.Close(); err != nil {
		return errors.Join(err, out.Close())
	}
	if err := out.Chmod(mode); err != nil {
		return errors.Join(err, out.Close())
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, target); err != nil {
		return err
	}
	complete = true
	return nil
}

// SnapshotKeys lists the keys whose manifests are present, in ascending order.
func (s *Store) SnapshotKeys() ([]cas.Digest, error) {
	entries, err := os.ReadDir(s.snapshots)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	keys := make([]cas.Digest, 0, len(entries))
	for _, entry := range entries {
		key, ok := manifestKey(entry.Name())
		if !ok || !entry.Type().IsRegular() {
			continue
		}
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b cas.Digest) int {
		return strings.Compare(a.Hex(), b.Hex())
	})
	return keys, nil
}

// RemoveSnapshot deletes one manifest. Its contents stay until a collection
// sweep finds them unreferenced.
func (s *Store) RemoveSnapshot(key cas.Digest) error {
	err := os.Remove(filepath.Join(s.snapshots, key.Hex()+snapshotManifestSuffix))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// SnapshotReferences reads every retained manifest and reports the object
// digests its entries need.
func (s *Store) SnapshotReferences(
	ctx context.Context,
	retained map[cas.Digest]struct{},
) (map[cas.Digest]struct{}, int, error) {
	keys, err := s.SnapshotKeys()
	if err != nil {
		return nil, 0, err
	}
	referenced := make(map[cas.Digest]struct{})
	removed := 0
	for _, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, removed, err
		}
		if _, keep := retained[key]; !keep {
			if err := s.RemoveSnapshot(key); err != nil {
				return nil, removed, fmt.Errorf("remove unreferenced bootstrap snapshot %s: %w", key.Hex(), err)
			}
			removed++
			continue
		}
		snap, ok, err := s.LookupSnapshot(ctx, key)
		if err != nil || !ok {
			if removeErr := s.RemoveSnapshot(key); removeErr != nil {
				return nil, removed, errors.Join(err, removeErr)
			}
			removed++
			continue
		}
		for _, entry := range snap.Entries {
			digest, parseErr := cas.ParseDigest("sha256:" + strings.ToLower(entry.SHA256))
			if parseErr != nil {
				return nil, removed, fmt.Errorf(
					"bootstrap snapshot %s entry %s: %w",
					key.Hex(), entry.Path, parseErr,
				)
			}
			referenced[digest] = struct{}{}
		}
	}
	return referenced, removed, nil
}

func (s *Store) readSnapshotManifest(key cas.Digest) ([]byte, bool, error) {
	path := filepath.Join(s.snapshots, key.Hex()+snapshotManifestSuffix)
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if !info.Mode().IsRegular() {
		return nil, false, fmt.Errorf("bootstrap snapshot %s is not a regular file", key.Hex())
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, snapshotManifestLimit+1))
	if err != nil {
		return nil, false, err
	}
	if len(raw) > snapshotManifestLimit {
		return nil, false, fmt.Errorf("bootstrap snapshot %s exceeds %d bytes", key.Hex(), snapshotManifestLimit)
	}
	return raw, true, nil
}

func (s *Store) writeSnapshotManifest(key cas.Digest, raw []byte) error {
	file, err := os.CreateTemp(s.snapshots, ".snapshot-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	complete := false
	defer func() {
		if !complete {
			_ = os.Remove(temporary)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return errors.Join(err, file.Close())
	}
	if _, err := file.Write(raw); err != nil {
		return errors.Join(err, file.Close())
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporary, filepath.Join(s.snapshots, key.Hex()+snapshotManifestSuffix)); err != nil {
		return err
	}
	complete = true
	return nil
}

func manifestKey(name string) (cas.Digest, bool) {
	hex, ok := strings.CutSuffix(name, snapshotManifestSuffix)
	if !ok {
		return cas.Digest{}, false
	}
	digest, err := cas.ParseDigest("sha256:" + hex)
	if err != nil {
		return cas.Digest{}, false
	}
	return digest, true
}

// createSnapshots creates the private namespace that holds snapshot manifests.
func createSnapshots(directory string) error {
	if err := fsutil.MkdirAllPrivate(directory); err != nil {
		return err
	}
	info, err := os.Lstat(directory)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("snapshots namespace must be a regular directory")
	}
	return nil
}
