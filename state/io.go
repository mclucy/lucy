package state

import (
	"fmt"
	"os"
	"path/filepath"
)

// StateFile identifies one persistent Lucy state file.
type StateFile string

const (
	// ManifestFile stores desired environment intent as YAML. It owns direct
	// roots, managed-scope declarations, and other descriptive statements about
	// what the project wants Lucy to converge toward. It may include optional
	// config overrides, but it must not contain lockfile-only fields such as
	// exact transitive closures, hashes, or exact download URLs.
	ManifestFile StateFile = "lucy.yaml"

	// LockFile stores the exact resolved graph and provenance for a manifest. It
	// owns exact versions, chosen sources, artifact identity, provenance chains,
	// and other reproducibility data. It must not become a dump of live probe
	// facts, user policy defaults, or procedural orchestration state.
	LockFile StateFile = "lucy-lock.yaml"
)

// AtomicWrite writes data to path via a temp file in the same directory and
// atomically renames it into place.
func AtomicWrite(path string, data []byte, perm os.FileMode) (err error) {
	dir := filepath.Dir(path)
	temp, err := os.CreateTemp(dir, filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("atomic write %s: create temp file: %w", path, err)
	}
	tempPath := temp.Name()
	defer func() {
		if err != nil {
			_ = os.Remove(tempPath)
		}
	}()

	if _, err = temp.Write(data); err != nil {
		_ = temp.Close()
		return fmt.Errorf("atomic write %s: write temp file: %w", path, err)
	}
	if err = temp.Chmod(perm); err != nil {
		_ = temp.Close()
		return fmt.Errorf("atomic write %s: chmod temp file: %w", path, err)
	}
	if err = temp.Close(); err != nil {
		return fmt.Errorf("atomic write %s: close temp file: %w", path, err)
	}
	if err = os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("atomic write %s: rename temp file: %w", path, err)
	}

	return nil
}

// SafeRead reads a file, treating a missing file as a non-error.
func SafeRead(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		return data, true, nil
	}
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	return nil, false, fmt.Errorf("read %s: %w", path, err)
}

// EnsureDir creates path and parents if they do not exist.
func EnsureDir(path string) error {
	if err := os.MkdirAll(path, 0o755); err != nil {
		return fmt.Errorf("ensure dir %s: %w", path, err)
	}
	return nil
}

// ReadManifest reads lucy.yaml from workDir if present.
// Manifest is the intent layer, including fuzzy versions and compatible-platform hints.
func ReadManifest(workDir string) (*Manifest, bool, error) {
	path := filepath.Join(workDir, string(ManifestFile))
	data, ok, err := SafeRead(path)
	if err != nil || !ok {
		return nil, ok, err
	}
	manifest, err := ParseManifest(data)
	if err != nil {
		return nil, false, err
	}
	return manifest, true, nil
}

// ReadLock reads lucy-lock.yaml from workDir if present.
// Lock is the exact fact layer for one resolved environment snapshot.
func ReadLock(workDir string) (*Lock, bool, error) {
	path := filepath.Join(workDir, string(LockFile))
	data, ok, err := SafeRead(path)
	if err != nil || !ok {
		return nil, ok, err
	}
	lock, err := ParseLock(data)
	if err != nil {
		return nil, false, err
	}
	return lock, true, nil
}

// WriteManifest writes lucy.yaml atomically.
// It preserves fuzzy intent instead of rewriting it to exact lock facts.
func WriteManifest(workDir string, m *Manifest) error {
	data, err := SerializeManifest(m)
	if err != nil {
		return err
	}
	return AtomicWrite(
		filepath.Join(workDir, string(ManifestFile)),
		data,
		0o600,
	)
}

// WriteLock writes lucy-lock.yaml atomically.
// It persists exact resolved environment and package facts.
func WriteLock(workDir string, l *Lock) error {
	data, err := SerializeLock(l)
	if err != nil {
		return err
	}
	return AtomicWrite(filepath.Join(workDir, string(LockFile)), data, 0o600)
}
