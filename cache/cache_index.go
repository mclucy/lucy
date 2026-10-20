package cache

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/internal/fsutil"
)

const indexVersion = 3

type indexManifest struct {
	Version int            `json:"version"`
	Entries map[key]*Entry `json:"entries"`
}

type index struct {
	path    string
	entries map[key]*Entry
	info    os.FileInfo
}

func newIndex(path string) *index {
	return &index{path: path, entries: make(map[key]*Entry)}
}

// load runs under cache ownership. Atomic replacement makes inode identity a safe fast path.
func (idx *index) load() (bool, error) {
	info, err := os.Stat(idx.path)
	if errors.Is(err, os.ErrNotExist) {
		idx.entries = make(map[key]*Entry)
		return true, idx.flush()
	}
	if err != nil {
		return false, err
	}
	if idx.info != nil && os.SameFile(info, idx.info) && info.Size() == idx.info.Size() && info.ModTime().Equal(idx.info.ModTime()) {
		return false, nil
	}
	file, err := os.Open(idx.path)
	if err != nil {
		return false, err
	}
	var manifest indexManifest
	decodeErr := json.UnmarshalRead(file, &manifest)
	info, statErr := file.Stat()
	closeErr := file.Close()
	if statErr != nil || closeErr != nil {
		return false, errors.Join(statErr, closeErr)
	}
	if decodeErr != nil || !validIndex(manifest) {
		idx.entries = make(map[key]*Entry)
		return true, idx.flush()
	}
	idx.info = info
	idx.entries = manifest.Entries
	return false, nil
}

func validIndex(manifest indexManifest) bool {
	if manifest.Version != indexVersion || manifest.Entries == nil {
		return false
	}
	for k, entry := range manifest.Entries {
		if entry == nil || entry.Size < 0 || entry.Kind > KindArtifact || entry.Key != string(k) {
			return false
		}
		digest, err := cas.ParseDigest(entry.ContentHash)
		if err != nil || entry.Filename == "" || sanitizeFilename(entry.Filename, digest.Hex()) != entry.Filename {
			return false
		}
	}
	return true
}

func (idx *index) flush() error {
	data, err := json.Marshal(indexManifest{
		Version: indexVersion,
		Entries: idx.entries,
	})
	if err != nil {
		idx.info = nil
		return fmt.Errorf("encode cache index: %w", err)
	}
	if err := fsutil.AtomicWrite(idx.path, data, 0o600); err != nil {
		idx.info = nil
		return fmt.Errorf("publish cache index: %w", err)
	}
	idx.info, err = os.Stat(idx.path)
	return err
}

func (idx *index) get(k key) (*Entry, bool) {
	entry, ok := idx.entries[k]
	return entry, ok
}

func (idx *index) put(k key, entry *Entry) {
	idx.entries[k] = entry
}

func (idx *index) delete(k key) {
	delete(idx.entries, k)
}

func (idx *index) exists(k key) bool {
	_, ok := idx.entries[k]
	return ok
}

func (idx *index) all() map[key]*Entry {
	return idx.entries
}
