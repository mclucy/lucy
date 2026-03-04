package cache

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
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
	file, err := os.Open(idx.path)
	if errors.Is(err, os.ErrNotExist) {
		return idx.create()
	} else if err != nil {
		return false
	}
	defer tools.CloseReader(file, logger.Warn)

	data, err := io.ReadAll(file)
	if err != nil {
		_ = resetCache(idx.path)
		return idx.create()
	}

	if idx.tryLoadV2(data) {
		return true
	}

	if idx.migrateFromV1(data) {
		if err := idx.flush(); err != nil {
			logger.Warn(fmt.Errorf("failed to persist migrated index: %w", err))
		}
		return true
	}

	_ = resetCache(idx.path)
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

func (idx *index) migrateFromV1(data []byte) bool {
	var legacy manifest
	if err := json.Unmarshal(data, &legacy); err != nil {
		return false
	}
	if legacy.Content == nil || legacy.LifeTime <= 0 || legacy.MaxSize <= 0 {
		return false
	}

	idx.entries = make(map[key]*CacheEntry, len(legacy.Content))
	for k, item := range legacy.Content {
		idx.entries[k] = &CacheEntry{
			Kind:        KindArtifact,
			Filename:    item.Filename,
			Size:        int64(item.Size),
			ContentHash: item.Hash,
			Integrity:   Integrity{State: IntegrityUnverified},
			Expiration:  item.Expiration,
			Key:         string(k),
			CreatedAt:   time.Time{},
		}
	}
	return true
}

func (idx *index) create() bool {
	dir := filepath.Dir(idx.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logger.Warn(fmt.Errorf("failed to create index directory %s: %w", dir, err))
		return false
	}
	idx.entries = make(map[key]*CacheEntry)
	if err := idx.flush(); err != nil {
		logger.Warn(fmt.Errorf("failed to write initial index: %w", err))
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
		logger.Warn(os.Remove(tempFile))
		return fmt.Errorf("failed to write temporary index file: %w", err)
	}

	if err := os.Rename(tempFile, idx.path); err != nil {
		logger.Warn(os.Remove(tempFile))
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
