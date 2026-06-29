package cache

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mclucy/lucy/log"
)

type handler struct {
	mu     sync.RWMutex
	on     bool
	dir    string
	store  *store
	index  *index
	policy Policy
}

func newHandler(name string, cfg CacheConfig) (obj *handler) {
	dir := setDir(name)
	obj = &handler{
		on:     cfg.Enabled,
		dir:    dir,
		store:  newStore(dir),
		policy: cfg.toPolicy(),
	}

	if !obj.on {
		return obj
	}

	if err := os.MkdirAll(obj.dir, 0o700); err != nil {
		log.Warn(
			fmt.Errorf(
				"cannot create cache directory, disabling %s cache: %w",
				name, err,
			),
		)
		obj.on = false
		return obj
	}

	idx := newIndex(filepath.Join(obj.dir, manifestFilename))
	if !idx.load() {
		obj.on = false
		return obj
	}
	obj.index = idx

	if obj.on {
		obj.clearExpiredCache()
		obj.maintainCacheLimit()
		if err := obj.index.flush(); err != nil {
			log.Warn(
				fmt.Errorf(
					"failed to update index on initialization: %w",
					err,
				),
			)
		}
	}

	return obj
}

func (handler *handler) Add(
	data []byte,
	filename string,
	k string,
	expiration time.Duration,
) error {
	if expiration == 0 {
		expiration = handler.policy.Artifact.TTL
	}
	return handler.AddEntry(
		data,
		filename,
		k,
		KindArtifact,
		Integrity{State: IntegrityUnverified},
		expiration,
	)
}

func (handler *handler) AddEntry(
	data []byte,
	filename string,
	k string,
	kind EntryKind,
	integrity Integrity,
	expiration time.Duration,
) error {
	handler.mu.Lock()
	defer handler.mu.Unlock()

	if !handler.on {
		return nil
	}

	if expiration == 0 {
		expiration = handler.policy.ConfigFor(kind).TTL
	}

	ckey := canonicalizeKey(k)
	contentHash := h(data)
	if filename == "" {
		filename = contentHash
	}
	filename = sanitizeFilename(filename, contentHash)

	if existing, ok := handler.index.get(ckey); ok {
		if existing.ContentHash == contentHash {
			return nil
		}
		_ = handler.store.Remove(existing.ContentHash)
		handler.index.delete(ckey)
	}

	if err := handler.store.Write(contentHash, filename, data); err != nil {
		return err
	}

	log.Debug(
		fmt.Sprintf(
			"cache store: %s (%s, %s)",
			k,
			kind,
			integrity.State,
		),
	)

	handler.index.put(
		ckey, &CacheEntry{
			Kind:        kind,
			Filename:    filename,
			Size:        int64(len(data)),
			ContentHash: contentHash,
			Integrity:   integrity,
			Expiration:  time.Now().Add(expiration),
			Key:         string(ckey),
			CreatedAt:   time.Now(),
		},
	)

	if err := handler.index.flush(); err != nil {
		return fmt.Errorf("failed to update index after adding item: %w", err)
	}
	return nil
}

// IngestEntry is a file-path variant of AddEntry for large files that should
// not be loaded into memory. The source file at srcPath is moved into the
// content-addressed store; contentHash must be pre-computed by the caller.
func (handler *handler) IngestEntry(
	srcPath string,
	filename string,
	k string,
	size int64,
	contentHash string,
	kind EntryKind,
	integrity Integrity,
	expiration time.Duration,
) error {
	handler.mu.Lock()
	defer handler.mu.Unlock()

	if !handler.on {
		return nil
	}

	if expiration == 0 {
		expiration = handler.policy.ConfigFor(kind).TTL
	}

	ckey := canonicalizeKey(k)
	if filename == "" {
		filename = contentHash
	}
	filename = sanitizeFilename(filename, contentHash)

	if existing, ok := handler.index.get(ckey); ok {
		if existing.ContentHash == contentHash {
			return nil
		}
		_ = handler.store.Remove(existing.ContentHash)
		handler.index.delete(ckey)
	}

	if err := handler.store.Ingest(contentHash, filename, srcPath); err != nil {
		return err
	}

	log.Debug(
		fmt.Sprintf(
			"cache ingest: %s (%s, %s)",
			k,
			kind,
			integrity.State,
		),
	)

	handler.index.put(
		ckey, &CacheEntry{
			Kind:        kind,
			Filename:    filename,
			Size:        size,
			ContentHash: contentHash,
			Integrity:   integrity,
			Expiration:  time.Now().Add(expiration),
			Key:         string(ckey),
			CreatedAt:   time.Now(),
		},
	)

	if err := handler.index.flush(); err != nil {
		return fmt.Errorf("failed to update index after ingesting item: %w", err)
	}
	return nil
}

func (handler *handler) Flush() error {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	if !handler.on {
		return nil
	}
	return handler.index.flush()
}

func (handler *handler) Exist(k string) bool {
	handler.mu.RLock()
	defer handler.mu.RUnlock()
	return handler.existLocked(k)
}

func (handler *handler) existLocked(k string) bool {
	if !handler.on {
		return false
	}
	return handler.index.exists(canonicalizeKey(k))
}

func (handler *handler) Get(k string) (hit bool, file *os.File, err error) {
	handler.mu.RLock()
	defer handler.mu.RUnlock()

	if !handler.on {
		return false, nil, nil
	}

	ckey := canonicalizeKey(k)
	entry, ok := handler.index.get(ckey)
	if !ok {
		log.Debug("cache miss: " + k)
		return false, nil, nil
	}

	file, err = handler.store.Read(entry.ContentHash, entry.Filename)
	if err != nil {
		return false, nil, err
	}
	log.Debug("cache hit: " + k)
	return true, file, nil
}

func (handler *handler) GetBytes(k string) (hit bool, data []byte, err error) {
	handler.mu.RLock()
	defer handler.mu.RUnlock()

	if !handler.on {
		return false, nil, nil
	}

	ckey := canonicalizeKey(k)
	entry, ok := handler.index.get(ckey)
	if !ok {
		log.Debug("cache miss: " + k)
		return false, nil, nil
	}

	data, err = handler.store.ReadBytes(entry.ContentHash, entry.Filename)
	if err != nil {
		return false, nil, err
	}
	log.Debug("cache hit: " + k)
	return true, data, nil
}

func (handler *handler) Remove(k string) error {
	handler.mu.Lock()
	defer handler.mu.Unlock()
	return handler.removeLocked(canonicalizeKey(k))
}

func (handler *handler) removeLocked(k key) error {
	if err := handler.removeEntryLocked(k); err != nil {
		return err
	}
	if err := handler.index.flush(); err != nil {
		return fmt.Errorf("failed to update index after removing item: %w", err)
	}
	return nil
}

func (handler *handler) removeEntryLocked(k key) error {
	if !handler.on {
		return nil
	}
	entry, ok := handler.index.get(k)
	if !ok {
		return nil
	}
	if err := handler.store.Remove(entry.ContentHash); err != nil {
		return err
	}
	handler.index.delete(k)
	return nil
}

func (handler *handler) All() []*CacheEntry {
	handler.mu.RLock()
	defer handler.mu.RUnlock()
	if !handler.on {
		return nil
	}
	entries := handler.index.all()
	result := make([]*CacheEntry, 0, len(entries))
	for _, entry := range entries {
		result = append(result, entry)
	}
	return result
}

func (handler *handler) ClearAll() (report ResetReport, err error) {
	handler.mu.Lock()
	defer handler.mu.Unlock()

	if !handler.on {
		return ResetReport{}, nil
	}

	if report, err = resetCache(handler.index.path, true); err != nil {
		return ResetReport{}, fmt.Errorf("failed to clear cache: %w", err)
	}
	log.Info("cache cleared")

	idx := newIndex(handler.index.path)
	if !idx.create() {
		return ResetReport{}, fmt.Errorf("failed to create new index after clearing cache")
	}
	handler.index = idx

	return report, nil
}
