package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/log"
)

const indexVersion = 2

type indexManifest struct {
	Version int                 `json:"version"`
	Entries map[key]*CacheEntry `json:"entries"`
}

type index struct {
	path    string
	entries map[key]*CacheEntry
}

func newIndex(manifestPath string) *index {
	idx := &index{
		path:    manifestPath,
		entries: make(map[key]*CacheEntry),
	}
	return idx
}

func (idx *index) load() bool {
	data, err := os.ReadFile(idx.path)
	if errors.Is(err, os.ErrNotExist) {
		return idx.create()
	}
	if err != nil {
		return false
	}

	if idx.tryLoadV2(data) {
		return true
	}

	_, _ = resetCache(idx.path, false)
	return idx.create()
}

func (idx *index) tryLoadV2(data []byte) bool {
	var m indexManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return false
	}
	if m.Version != indexVersion || m.Entries == nil {
		return false
	}
	idx.entries = m.Entries
	return true
}

func (idx *index) create() bool {
	dir := filepath.Dir(idx.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Warn(
			fmt.Errorf(
				"failed to create index directory %s: %w",
				dir,
				err,
			),
		)
		return false
	}
	idx.entries = make(map[key]*CacheEntry)
	if err := idx.flush(); err != nil {
		log.Warn(fmt.Errorf("failed to write initial index: %w", err))
		return false
	}
	return true
}

func (idx *index) flush() error {
	m := indexManifest{
		Version: indexVersion,
		Entries: idx.entries,
	}
	data, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("failed to marshal index: %w", err)
	}

	tempFile := idx.path + ".tmp"
	if err := os.WriteFile(tempFile, data, 0o600); err != nil {
		log.Warn(os.Remove(tempFile))
		return fmt.Errorf("failed to write temporary index file: %w", err)
	}

	if err := os.Rename(tempFile, idx.path); err != nil {
		log.Warn(os.Remove(tempFile))
		return fmt.Errorf("failed to replace index file: %w", err)
	}
	return nil
}

func (idx *index) get(k key) (*CacheEntry, bool) {
	e, ok := idx.entries[k]
	return e, ok
}

func (idx *index) put(k key, entry *CacheEntry) {
	idx.entries[k] = entry
}

func (idx *index) delete(k key) {
	delete(idx.entries, k)
}

func (idx *index) exists(k key) bool {
	_, ok := idx.entries[k]
	return ok
}

func (idx *index) all() map[key]*CacheEntry {
	return idx.entries
}
