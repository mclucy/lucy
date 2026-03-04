package cache

import (
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/mclucy/lucy/logger"
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
		logger.Warn(
			fmt.Errorf(
				"cannnot create cache directory, disabling %s cache: %w",
				name, err,
			),
		)
		obj.on = false
		return obj
	}

	idx := newIndex(fmt.Sprintf("%s/%s", obj.dir, manifestFilename))
	if !idx.load() {
		obj.on = false
		return obj
	}
	obj.index = idx

	if obj.on {
		obj.clearExpiredCache()
		obj.maintainCacheLimit()
		if err := obj.index.flush(); err != nil {
			logger.Warn(
				fmt.Errorf(
					"failed to update index on initialization: %w",
					err,
				),
			)
		}
	}

	return obj
}

func (h *handler) Add(
	data []byte,
	filename string,
	k string,
	expiration time.Duration,
) error {
	if expiration == 0 {
		expiration = h.policy.Artifact.TTL
	}
	return h.AddEntry(data, filename, k, KindArtifact, Integrity{State: IntegrityUnverified}, expiration)
}

func (h *handler) AddEntry(
	data []byte,
	filename string,
	k string,
	kind EntryKind,
	integrity Integrity,
	expiration time.Duration,
) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.on {
		return nil
	}

	if expiration == 0 {
		expiration = h.policy.ConfigFor(kind).TTL
	}

	ckey := canonicalizeKey(k)
	contentHash := hash(data)
	if filename == "" {
		filename = contentHash
	}
	filename = sanitizeFilename(filename, contentHash)

	if existing, ok := h.index.get(ckey); ok {
		if existing.ContentHash == contentHash {
			return nil
		}
		_ = h.store.Remove(existing.ContentHash)
		h.index.delete(ckey)
	}

	if err := h.store.Write(contentHash, filename, data); err != nil {
		return err
	}

	h.index.put(ckey, &CacheEntry{
		Kind:        kind,
		Filename:    filename,
		Size:        int64(len(data)),
		ContentHash: contentHash,
		Integrity:   integrity,
		Expiration:  time.Now().Add(expiration),
		Key:         string(ckey),
		CreatedAt:   time.Now(),
	})

	if err := h.index.flush(); err != nil {
		logger.Warn(
			fmt.Errorf("failed to update index after adding item: %w", err),
		)
	}
	return nil
}

// IngestEntry is a file-path variant of AddEntry for large files that should
// not be loaded into memory. The source file at srcPath is moved into the
// content-addressed store; contentHash must be pre-computed by the caller.
func (h *handler) IngestEntry(
	srcPath string,
	filename string,
	k string,
	size int64,
	contentHash string,
	kind EntryKind,
	integrity Integrity,
	expiration time.Duration,
) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.on {
		return nil
	}

	if expiration == 0 {
		expiration = h.policy.ConfigFor(kind).TTL
	}

	ckey := canonicalizeKey(k)
	if filename == "" {
		filename = contentHash
	}
	filename = sanitizeFilename(filename, contentHash)

	if existing, ok := h.index.get(ckey); ok {
		if existing.ContentHash == contentHash {
			return nil
		}
		_ = h.store.Remove(existing.ContentHash)
		h.index.delete(ckey)
	}

	if err := h.store.Ingest(contentHash, filename, srcPath); err != nil {
		return err
	}

	h.index.put(ckey, &CacheEntry{
		Kind:        kind,
		Filename:    filename,
		Size:        size,
		ContentHash: contentHash,
		Integrity:   integrity,
		Expiration:  time.Now().Add(expiration),
		Key:         string(ckey),
		CreatedAt:   time.Now(),
	})

	if err := h.index.flush(); err != nil {
		logger.Warn(
			fmt.Errorf("failed to update index after ingesting item: %w", err),
		)
	}
	return nil
}

func (h *handler) Flush() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.on {
		return nil
	}
	return h.index.flush()
}

func (h *handler) Exist(k string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.existLocked(k)
}

func (h *handler) existLocked(k string) bool {
	if !h.on {
		return false
	}
	return h.index.exists(canonicalizeKey(k))
}

func (h *handler) Get(k string) (hit bool, file *os.File, err error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if !h.on {
		return false, nil, nil
	}

	ckey := canonicalizeKey(k)
	entry, ok := h.index.get(ckey)
	if !ok {
		return false, nil, nil
	}

	file, err = h.store.Read(entry.ContentHash, entry.Filename)
	if err != nil {
		return false, nil, err
	}
	return true, file, nil
}

func (h *handler) Remove(k string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.removeLocked(canonicalizeKey(k))
}

func (h *handler) removeLocked(k key) error {
	if err := h.removeEntryLocked(k); err != nil {
		return err
	}
	if err := h.index.flush(); err != nil {
		logger.Warn(
			fmt.Errorf("failed to update index after removing item: %w", err),
		)
	}
	return nil
}

func (h *handler) removeEntryLocked(k key) error {
	if !h.on {
		return nil
	}
	entry, ok := h.index.get(k)
	if !ok {
		return nil
	}
	if err := h.store.Remove(entry.ContentHash); err != nil {
		return err
	}
	h.index.delete(k)
	return nil
}

func (h *handler) ClearAll() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.on {
		return nil
	}

	if err := resetCache(h.index.path); err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	idx := newIndex(h.index.path)
	if !idx.create() {
		return fmt.Errorf("failed to create new index after clearing cache")
	}
	h.index = idx

	return nil
}
