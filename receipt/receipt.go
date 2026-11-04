// Package receipt owns the workspace installation receipt: the ledger of
// committed runtime outputs and the resolution it answers.
package receipt

import (
	"bytes"
	"io"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/internal/fsutil"
	"gopkg.in/yaml.v3"
)

const (
	// Filename is the receipt's path inside a workspace.
	Filename = ".lucy/install.yaml"
	// Version is the schema version this build reads and writes.
	Version = 1
)

// Receipt records the outputs an installation committed. It is not a second
// lock: paths and digests describe local state, and the fingerprint names the
// resolution that produced it.
type Receipt struct {
	Version         int     `yaml:"version"`
	LockFingerprint string  `yaml:"lock_fingerprint"`
	Entries         []Entry `yaml:"entries"`
}

// Entry records one committed output.
type Entry struct {
	Path   string `yaml:"path"`
	SHA256 string `yaml:"sha256"`
	Mode   uint32 `yaml:"mode"`
}

// New returns an empty receipt for the current schema.
func New() Receipt {
	return Receipt{Version: Version, Entries: []Entry{}}
}

// Decode parses receipt bytes without checking the version. A caller that
// understands only some versions checks the field after decoding.
func Decode(reader io.Reader) (*Receipt, error) {
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	var result Receipt
	if err := yaml.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ReadAt reads the receipt at path. A missing file returns an error that
// matches os.ErrNotExist.
func ReadAt(path string) (*Receipt, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return Decode(bytes.NewReader(raw))
}

// Read reads the receipt of the workspace at root.
func Read(root string) (*Receipt, error) {
	return ReadAt(filepath.Join(root, Filename))
}

// Snapshot returns the receipt bytes without writing them.
func (r *Receipt) Snapshot() ([]byte, error) {
	result := *r
	if result.Entries == nil {
		result.Entries = []Entry{}
	}
	return yaml.Marshal(result)
}

// WriteAt replaces the receipt at path atomically. A caller that must publish
// the bytes inside a larger transaction takes a Snapshot instead.
func (r *Receipt) WriteAt(path string) error {
	data, err := r.Snapshot()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fsutil.AtomicWrite(path, data, 0o600)
}

// Write replaces the receipt of the workspace at root atomically.
func (r *Receipt) Write(root string) error {
	return r.WriteAt(filepath.Join(root, Filename))
}
