// Package serverregistry locates named workspaces through per-user registry
// entries that record canonical workspace paths.
package serverregistry

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
)

const (
	StateAvailable  = "available"
	StateLockOnly   = "lock-only"
	StateMissing    = "missing"
	StateUnreadable = "unreadable"
	StateInvalid    = "invalid"
)

// Entry describes a registered path and its current declaration. An unnamed
// server has no name selector. Unavailable entries remain registered.
type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	State   string `json:"state"`
	Problem string `json:"problem,omitempty"`
}

type Registry struct {
	directory string
}

const registryLockName = "registry.lock"

const (
	// entryTempPrefix names the staged file a registration publishes. List
	// ignores it, so an abandoned stage is inert debris rather than an entry.
	entryTempPrefix = ".tmp-"
	// maxEntrySize bounds how much of an entry file List will read.
	maxEntrySize = 64 << 10
)

// New locates durable per-user data without creating or changing the registry.
func New() (*Registry, error) {
	root, err := DataRoot()
	if err != nil {
		return nil, err
	}
	return &Registry{directory: filepath.Join(root, "servers")}, nil
}

// DataRoot reports the per-user directory Lucy owns for durable machine state.
// It matches the platform's local application data conventions. It never
// creates the directory.
func DataRoot() (string, error) {
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
		if base == "" {
			return "", errors.New("locate lucy data directory: LOCALAPPDATA is not set")
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate lucy data directory: %w", err)
		}
		base = filepath.Join(home, "Library", "Application Support")
	default:
		base = os.Getenv("XDG_DATA_HOME")
		if !filepath.IsAbs(base) {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("locate lucy data directory: %w", err)
			}
			base = filepath.Join(home, ".local", "share")
		}
	}
	return filepath.Join(base, "lucy"), nil
}

// List checks every target on each call. It does not prune entries: a missing
// target can be an unmounted volume, and deletion could race registration. An
// entry that cannot be interpreted is reported as invalid, which makes
// collection refuse to run.
func (r *Registry) List() ([]Entry, error) {
	links, err := os.ReadDir(r.directory)
	if errors.Is(err, os.ErrNotExist) {
		return []Entry{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read server registry: %w", err)
	}
	entries := make([]Entry, 0, len(links))
	for _, link := range links {
		if link.Name() == registryLockName ||
			strings.HasPrefix(link.Name(), entryTempPrefix) {
			continue
		}
		path := filepath.Join(r.directory, link.Name())
		target, err := readEntry(path)
		if errors.Is(err, os.ErrNotExist) {
			// A concurrent deinitialization removed this entry.
			continue
		}
		if err != nil {
			entries = append(entries, Entry{
				Path:    path,
				State:   StateInvalid,
				Problem: err.Error(),
			})
			continue
		}
		entries = append(entries, observe(target))
	}
	slices.SortFunc(entries, func(a, b Entry) int {
		return cmp.Or(strings.Compare(a.Name, b.Name), strings.Compare(a.Path, b.Path))
	})
	return entries, nil
}

// Resolve requires exactly one available declaration with this name.
func (r *Registry) Resolve(name string) (Entry, error) {
	name, err := document.NormalizeName(name)
	if err != nil {
		return Entry{}, err
	}
	entries, err := r.List()
	if err != nil {
		return Entry{}, err
	}
	var match Entry
	count := 0
	for _, entry := range entries {
		if entry.State == StateAvailable && entry.Name == name {
			match = entry
			count++
		}
	}
	switch count {
	case 0:
		return Entry{}, fmt.Errorf("no available registered server named %q; run `lucy server ls` to inspect registered paths", name)
	case 1:
		return match, nil
	default:
		var message strings.Builder
		fmt.Fprintf(&message, "server name %q is ambiguous; matching directories:", name)
		for _, entry := range entries {
			if entry.State == StateAvailable && entry.Name == name {
				fmt.Fprintf(&message, "\n  %s", entry.Path)
			}
		}
		message.WriteString("\nrun Lucy in the intended directory or give its manifest a unique name")
		return Entry{}, errors.New(message.String())
	}
}

// Register creates one entry per canonical workspace path for a workspace with
// a readable manifest. The caller must hold the workspace writer lock so
// deinitialization cannot remove a newer entry. Other available entries with
// the same name are returned for presentation.
func (r *Registry) Register(root string) ([]Entry, error) {
	root, err := canonical(root)
	if err != nil {
		return nil, err
	}
	entry := observe(root)
	if entry.State != StateAvailable {
		return nil, fmt.Errorf("register server %s: %s", root, entry.Problem)
	}
	return r.registerEntry(root, entry)
}

// RegisterInstallation records a workspace whose installation completed. A
// workspace with a readable manifest is registered by name. A lock-only
// installation has no manifest name but still pins artifacts, so it is
// recorded as a lock-only entry that collection tracks and --server does not
// select. The caller must hold the workspace writer lock.
func (r *Registry) RegisterInstallation(root string) ([]Entry, error) {
	root, err := canonical(root)
	if err != nil {
		return nil, err
	}
	entry := observe(root)
	switch entry.State {
	case StateAvailable, StateLockOnly:
	default:
		return nil, fmt.Errorf("register server %s: %s", root, entry.Problem)
	}
	return r.registerEntry(root, entry)
}

func (r *Registry) registerEntry(root string, entry Entry) ([]Entry, error) {
	lock, err := r.lock()
	if err != nil {
		return nil, fmt.Errorf("lock server registry: %w", err)
	}
	defer func() { _ = lock.Close() }()
	entryPath := filepath.Join(r.directory, pathKey(root))
	target, err := readEntry(entryPath)
	switch {
	case err == nil:
		if !samePath(target, root) {
			return nil, fmt.Errorf("server registry entry %s does not point to %s", entryPath, root)
		}
	case errors.Is(err, os.ErrNotExist):
		if err := writeEntry(entryPath, root); err != nil {
			return nil, fmt.Errorf("register server directory entry: %w", err)
		}
	default:
		return nil, fmt.Errorf("read server registry entry %s: %w", entryPath, err)
	}
	entries, err := r.List()
	if err != nil {
		return nil, err
	}
	if entry.Name == "" {
		return nil, nil
	}
	duplicates := entries[:0]
	for _, other := range entries {
		if other.State == StateAvailable && other.Name == entry.Name && !samePath(other.Path, root) {
			duplicates = append(duplicates, other)
		}
	}
	return duplicates, nil
}

// Unregister removes only this path's entry, never the target directory. It
// must run while the caller still holds the workspace's original writer lock.
func (r *Registry) Unregister(root string) error {
	root, err := canonical(root)
	if err != nil {
		return err
	}
	if _, err := os.Stat(r.directory); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect server registry: %w", err)
	}
	lock, err := r.lock()
	if err != nil {
		return fmt.Errorf("lock server registry: %w", err)
	}
	defer func() { _ = lock.Close() }()
	entryPath := filepath.Join(r.directory, pathKey(root))
	target, err := readEntry(entryPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read server registry entry: %w", err)
	}
	if !samePath(target, root) {
		return fmt.Errorf("server registry entry %s does not point to %s", entryPath, root)
	}
	if err := os.Remove(entryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("unregister server: %w", err)
	}
	return nil
}

// Lock holds the registry mutation lock. Registration and unregistration use
// the same lock, so a caller can observe a stable registered set while it
// holds this lock. The lock file lives beside the entries and is skipped by
// List.
func (r *Registry) Lock() (*fsutil.FileLock, error) {
	return r.lock()
}

func (r *Registry) lock() (*fsutil.FileLock, error) {
	if err := fsutil.MkdirAllPrivate(r.directory); err != nil {
		return nil, fmt.Errorf("create server registry: %w", err)
	}
	return fsutil.LockFile(filepath.Join(r.directory, registryLockName))
}

func observe(root string) Entry {
	entry := Entry{Path: root, State: StateAvailable}
	info, err := os.Stat(root)
	if err != nil {
		entry.State = StateUnreadable
		if errors.Is(err, os.ErrNotExist) {
			entry.State = StateMissing
		}
		entry.Problem = err.Error()
		return entry
	}
	if !info.IsDir() {
		entry.State = StateInvalid
		entry.Problem = "registered target is not a directory"
		return entry
	}
	manifest, err := document.ManifestFile(root).Read()
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if _, lockErr := document.LockFile(root).Read(); lockErr == nil {
				entry.State = StateLockOnly
				return entry
			} else if !errors.Is(lockErr, os.ErrNotExist) {
				entry.State = StateUnreadable
				entry.Problem = "read lockfile: " + lockErr.Error()
				return entry
			}
		}
		entry.State = StateInvalid
		if _, pathError := errors.AsType[*os.PathError](err); pathError && !errors.Is(err, os.ErrNotExist) {
			entry.State = StateUnreadable
		}
		entry.Problem = err.Error()
		return entry
	}
	entry.Name = manifest.Name
	return entry
}

func readEntry(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("registry entry is not a regular file")
	}
	if info.Size() > maxEntrySize {
		return "", fmt.Errorf("registry entry exceeds %d bytes", maxEntrySize)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(string(data)) {
		return "", errors.New("registry entry does not contain an absolute path")
	}
	return string(data), nil
}

// writeEntry publishes a complete entry in one rename, so an unlocked List
// observes either the previous entry or the full target, never a partial file.
// An abandoned stage keeps the prefix List ignores.
func writeEntry(path, target string) (resultErr error) {
	staged, err := os.CreateTemp(filepath.Dir(path), entryTempPrefix)
	if err != nil {
		return fmt.Errorf("stage server registry entry: %w", err)
	}
	stagedPath := staged.Name()
	defer func() {
		if resultErr != nil {
			_ = os.Remove(stagedPath)
		}
	}()
	if _, err := staged.WriteString(target); err != nil {
		return errors.Join(err, staged.Close())
	}
	if err := staged.Sync(); err != nil {
		return errors.Join(err, staged.Close())
	}
	if err := staged.Close(); err != nil {
		return err
	}
	if err := os.Rename(stagedPath, path); err != nil {
		return fmt.Errorf("publish server registry entry: %w", err)
	}
	return nil
}

func canonical(root string) (string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve server directory: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve server directory: %w", err)
	}
	return resolved, nil
}

func pathKey(root string) string {
	if runtime.GOOS == "windows" {
		root = strings.ToLower(root)
	}
	digest := sha256.Sum256([]byte(root))
	return hex.EncodeToString(digest[:])
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
