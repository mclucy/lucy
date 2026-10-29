package artifactstore

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/serverregistry"
)

// Report describes one garbage collection. Bytes counts the reclaimed object
// contents, not staging files cleaned separately.
type Report struct {
	Removed int   `json:"removed"`
	Bytes   int64 `json:"bytes"`
}

// GC removes objects that no registered server's lock pins. It refuses to
// delete anything when an operation lease is active, when the registry or any
// registered workspace cannot report a complete reference set, when another
// Lucy operation owns a registered workspace, or when a lock names an artifact
// without a usable sha256 digest.
//
// Locks are acquired in one order everywhere: operation gate, workspace writer
// locks, registry mutation lock, then the content store lock. An operation
// creates its lease before taking its workspace lock. A sweep holds the
// operation gate from the lease check through the last removal, so no
// acquisition can start while references are read or objects are deleted.
func (s *Store) GC(registry *serverregistry.Registry) (Report, error) {
	if registry == nil {
		return Report{}, errors.New("server registry is unavailable")
	}
	gate, err := s.beginCollection()
	if err != nil {
		return Report{}, err
	}
	defer func() { _ = gate.Close() }()

	entries, err := registry.List()
	if err != nil {
		return Report{}, fmt.Errorf("inspect server registry: %w", err)
	}
	validated, err := validatedEntries(entries)
	if err != nil {
		return Report{}, err
	}
	workspaceLocks, err := lockWorkspaces(validated)
	if err != nil {
		return Report{}, err
	}
	defer func() {
		for i := len(workspaceLocks) - 1; i >= 0; i-- {
			_ = workspaceLocks[i].Close()
		}
	}()

	registryLock, err := registry.Lock()
	if err != nil {
		return Report{}, fmt.Errorf("lock server registry: %w", err)
	}
	defer func() { _ = registryLock.Close() }()
	refreshed, err := registry.List()
	if err != nil {
		return Report{}, fmt.Errorf("refresh server registry: %w", err)
	}
	if !sameRegistrySet(validated, refreshed) {
		return Report{}, errors.New("registered server set changed during collection; retry")
	}

	referenced, err := lockReferences(validated)
	if err != nil {
		return Report{}, err
	}
	var report Report
	removed, bytes, err := s.Store.Sweep(func() (map[cas.Digest]struct{}, func() error, error) {
		return referenced, nil, nil
	})
	report.Removed = removed
	report.Bytes = bytes
	if err != nil {
		return report, fmt.Errorf("collect artifact store: %w", err)
	}
	if err := s.Cleanup(); err != nil {
		return report, fmt.Errorf("clean artifact store staging: %w", err)
	}
	return report, nil
}

// validatedEntries rejects every state whose references cannot be determined
// and returns one entry per canonical path. Lock-only entries are valid: a
// lock-only installation is still a registered server whose lock pins
// artifacts.
func validatedEntries(entries []serverregistry.Entry) ([]serverregistry.Entry, error) {
	validated := make([]serverregistry.Entry, 0, len(entries))
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		switch entry.State {
		case serverregistry.StateAvailable, serverregistry.StateLockOnly:
		default:
			problem := entry.Problem
			if problem == "" {
				problem = "the workspace cannot be inspected"
			}
			return nil, fmt.Errorf(
				"registered server %s is %s: %s",
				displayEntry(entry), entry.State, problem,
			)
		}
		if _, duplicate := seen[entry.Path]; duplicate {
			continue
		}
		seen[entry.Path] = struct{}{}
		validated = append(validated, entry)
	}
	sort.Slice(validated, func(i, j int) bool {
		return validated[i].Path < validated[j].Path
	})
	return validated, nil
}

// lockWorkspaces takes every registered workspace's writer lock without
// creating or changing workspace state. An active operation is a reason to
// refuse collection, not a reason to wait: reference state would keep moving
// while it completed.
func lockWorkspaces(entries []serverregistry.Entry) ([]*fsutil.FileLock, error) {
	locks := make([]*fsutil.FileLock, 0, len(entries))
	fail := func(reason error) ([]*fsutil.FileLock, error) {
		for i := len(locks) - 1; i >= 0; i-- {
			_ = locks[i].Close()
		}
		return nil, reason
	}
	for _, entry := range entries {
		lock, err := lockWorkspace(entry.Path)
		if err != nil {
			return fail(fmt.Errorf(
				"registered server %s cannot be held still: %w",
				displayEntry(entry), err,
			))
		}
		locks = append(locks, lock)
	}
	return locks, nil
}

func lockWorkspace(root string) (*fsutil.FileLock, error) {
	relative := ".lucy/" + workspacewrite.LockFileName
	if err := workspacewrite.SafePath(root, relative); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filepath.Join(root, filepath.FromSlash(relative)), os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, errors.New("workspace writer lock is missing")
	}
	if err != nil {
		return nil, err
	}
	lock, err := fsutil.LockOpenedFile(file, false)
	if errors.Is(err, fsutil.ErrLocked) {
		return nil, errors.New("another Lucy operation is active")
	}
	return lock, err
}

func sameRegistrySet(expected, current []serverregistry.Entry) bool {
	currentByPath := make(map[string]string, len(current))
	for _, entry := range current {
		currentByPath[entry.Path] = entry.State
	}
	for _, entry := range expected {
		if state, ok := currentByPath[entry.Path]; !ok || state != entry.State {
			return false
		}
	}
	return len(currentByPath) == len(expected)
}

func displayEntry(entry serverregistry.Entry) string {
	if entry.Name != "" {
		return fmt.Sprintf("%q (%s)", entry.Name, entry.Path)
	}
	return entry.Path
}

func lockReferences(entries []serverregistry.Entry) (map[cas.Digest]struct{}, error) {
	referenced := make(map[cas.Digest]struct{})
	for _, entry := range entries {
		journal := filepath.Join(entry.Path, workspacewrite.InstallJournalName)
		if _, err := os.Lstat(journal); err == nil {
			return nil, fmt.Errorf("registered server %s has an interrupted installation; run `lucy install` in that workspace before collecting", displayEntry(entry))
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("inspect installation journal for registered server %s: %w", displayEntry(entry), err)
		}
		locked, err := document.LockFile(entry.Path).Read()
		if errors.Is(err, os.ErrNotExist) {
			// A registered workspace without a lock pins no artifacts.
			continue
		}
		if err != nil {
			return nil, fmt.Errorf(
				"registered server %s has an unreadable lock: %w",
				displayEntry(entry), err,
			)
		}
		if err := addLockReferences(referenced, entry, locked); err != nil {
			return nil, err
		}
	}
	return referenced, nil
}

func addLockReferences(
	referenced map[cas.Digest]struct{},
	entry serverregistry.Entry,
	locked *document.Lock,
) error {
	owner := displayEntry(entry)
	if err := addArtifactReference(referenced, owner, "server core", locked.Server.Artifact); err != nil {
		return err
	}
	for _, input := range locked.Server.Inputs {
		if err := addArtifactReference(referenced, owner, "bootstrap input "+input.Path, input.Artifact); err != nil {
			return err
		}
	}
	for _, pkg := range locked.Packages {
		name := "package " + pkg.ProjectID
		if pkg.Version != "" {
			name += "@" + pkg.Version.String()
		}
		if err := addArtifactReference(referenced, owner, name, pkg.Artifact); err != nil {
			return err
		}
	}
	return nil
}

func addArtifactReference(
	referenced map[cas.Digest]struct{},
	owner, name string,
	artifact catalog.Artifact,
) error {
	if artifact.Embedded != "" {
		// Embedded inputs are extracted from their pinned parent object.
		return nil
	}
	value := strings.ToLower(strings.TrimSpace(artifact.Hashes["sha256"]))
	if value == "" {
		return fmt.Errorf(
			"server %s: %s has no sha256 digest; its store object cannot be determined",
			owner, name,
		)
	}
	digest, err := cas.ParseDigest("sha256:" + value)
	if err != nil {
		return fmt.Errorf(
			"server %s: %s has an invalid sha256 digest %q: %w",
			owner, name, value, err,
		)
	}
	referenced[digest] = struct{}{}
	return nil
}
