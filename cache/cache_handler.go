package cache

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/log"
)

var ErrDisabled = errors.New("cache is disabled")

type handler struct {
	mu     sync.Mutex
	on     bool
	dir    string
	store  *cas.Store
	index  *index
	policy Policy
}

func newHandler(name string, cfg CacheConfig) *handler {
	h := &handler{on: cfg.Enabled, dir: setDir(name), policy: cfg.toPolicy()}
	if !h.on {
		return h
	}
	if err := h.initialize(cfg); err != nil {
		log.Warn(fmt.Errorf("disable %s cache: %w", name, err))
		h.on = false
		if h.store != nil {
			if err := h.store.Close(); err != nil {
				log.Warn(err)
			}
		}
	}
	return h
}

func (h *handler) initialize(cfg CacheConfig) (resultErr error) {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := fsutil.MkdirAllPrivate(h.dir); err != nil {
		return err
	}
	lock, err := fsutil.LockFile(h.dir + ".lock")
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	h.store, err = cas.New(filepath.Join(h.dir, "cas"))
	if err != nil {
		return err
	}
	h.index = newIndex(filepath.Join(h.dir, manifestFilename))
	reset, err := h.index.load()
	if err != nil {
		return err
	}
	if reset {
		if err := clearLegacyCache(h.dir); err != nil {
			return err
		}
	}
	removed, err := h.maintainLocked(nil)
	if err != nil {
		return err
	}
	if len(removed) != 0 {
		if err := h.index.flush(); err != nil {
			return err
		}
	}
	if err := h.collectAllLocked(nil); err != nil {
		return err
	}
	return h.store.Cleanup()
}

func (h *handler) lockIndex() (*fsutil.FileLock, error) {
	if !h.on {
		return nil, ErrDisabled
	}
	lock, err := fsutil.LockFile(h.dir + ".lock")
	if err != nil {
		return nil, err
	}
	reset, err := h.index.load()
	if err == nil && reset {
		err = clearLegacyCache(h.dir)
		if err == nil {
			err = h.collectAllLocked(nil)
		}
	}
	if err != nil {
		return nil, errors.Join(err, lock.Close())
	}
	return lock, nil
}

func (h *handler) newWriter() (*cas.Writer, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.on {
		return nil, ErrDisabled
	}
	return h.store.NewWriter()
}

func (h *handler) Add(data []byte, filename, k string, expiration time.Duration) error {
	return h.AddEntry(data, filename, k, KindArtifact, Integrity{State: IntegrityUnverified}, expiration)
}

func (h *handler) AddEntry(data []byte, filename, k string, kind EntryKind, integrity Integrity, expiration time.Duration) (resultErr error) {
	writer, err := h.newWriter()
	if errors.Is(err, ErrDisabled) {
		return nil
	}
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Abort()) }()
	if _, err := io.Copy(writer, bytes.NewReader(data)); err != nil {
		return err
	}
	if _, err := writer.Seal(nil); err != nil {
		return err
	}
	return h.CommitEntry(writer, filename, k, kind, integrity, expiration)
}

// CommitEntry publishes bytes before their reference and reclaims only after the index commits.
func (h *handler) CommitEntry(writer *cas.Writer, filename, k string, kind EntryKind, integrity Integrity, expiration time.Duration) (resultErr error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	lock, err := h.lockIndex()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	object, err := writer.Commit(nil)
	if err != nil {
		return err
	}
	if expiration == 0 {
		expiration = h.policy.ConfigFor(kind).TTL
	}
	ckey := canonicalizeKey(k)
	now := time.Now()
	created := now
	contentHash := object.Digest.String()
	var removed []cas.Digest
	if old, ok := h.index.get(ckey); ok {
		created = old.CreatedAt
		if old.ContentHash != contentHash {
			digest, err := cas.ParseDigest(old.ContentHash)
			if err != nil {
				return err
			}
			removed = append(removed, digest)
		}
	}
	h.index.put(ckey, &CacheEntry{Kind: kind, Filename: sanitizeFilename(filename, object.Digest.Hex()), Size: object.Size, ContentHash: contentHash, Integrity: integrity, Expiration: now.Add(expiration), Key: string(ckey), CreatedAt: created})
	removed, err = h.maintainLocked(removed)
	if err != nil {
		return err
	}
	if err := h.index.flush(); err != nil {
		return err
	}
	return h.collectCandidatesLocked(removed)
}

func (h *handler) Flush() (resultErr error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.on {
		return nil
	}
	lock, err := h.lockIndex()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	return h.index.flush()
}

func (h *handler) Exist(k string) bool {
	hit, reader, _, err := h.Get(k)
	if err != nil {
		log.Warn(err)
		return false
	}
	if reader != nil {
		if err := reader.Close(); err != nil {
			log.Warn(err)
			return false
		}
	}
	return hit
}

// Get keeps a reader alive independently of later index replacements. The caller must Close it.
func (h *handler) Get(k string) (hit bool, reader io.ReadCloser, filename string, resultErr error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.on {
		return false, nil, "", nil
	}
	lock, err := h.lockIndex()
	if err != nil {
		return false, nil, "", err
	}
	defer func() {
		resultErr = errors.Join(resultErr, lock.Close())
		if resultErr != nil && reader != nil {
			resultErr = errors.Join(resultErr, reader.Close())
			hit, reader, filename = false, nil, ""
		}
	}()
	entry, ok := h.index.get(canonicalizeKey(k))
	if !ok || !entry.Expiration.After(time.Now()) {
		return false, nil, "", nil
	}
	digest, err := cas.ParseDigest(entry.ContentHash)
	if err != nil {
		return false, nil, "", err
	}
	reader, err = h.store.Open(digest)
	if err != nil {
		return false, nil, "", err
	}
	return true, reader, entry.Filename, nil
}

func (h *handler) GetBytes(k string) (hit bool, data []byte, resultErr error) {
	hit, reader, _, err := h.Get(k)
	if err != nil || !hit {
		return false, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, reader.Close()) }()
	data, err = io.ReadAll(reader)
	if err != nil {
		return false, nil, err
	}
	return true, data, nil
}

func (h *handler) Remove(k string) (resultErr error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.on {
		return nil
	}
	lock, err := h.lockIndex()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	entry, ok := h.index.get(canonicalizeKey(k))
	if !ok {
		return nil
	}
	digest, err := cas.ParseDigest(entry.ContentHash)
	if err != nil {
		return err
	}
	h.index.delete(canonicalizeKey(k))
	if err := h.index.flush(); err != nil {
		return err
	}
	return h.collectCandidatesLocked([]cas.Digest{digest})
}

func (h *handler) All() []*CacheEntry {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.on {
		return nil
	}
	lock, err := h.lockIndex()
	if err != nil {
		log.Warn(err)
		return nil
	}
	defer func() {
		if err := lock.Close(); err != nil {
			log.Warn(err)
		}
	}()
	keys := slices.Sorted(maps.Keys(h.index.entries))
	entries := make([]*CacheEntry, 0, len(keys))
	for _, k := range keys {
		entry := *h.index.entries[k]
		entries = append(entries, &entry)
	}
	return entries
}

func (h *handler) ClearAll() (report ResetReport, resultErr error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.on {
		return report, nil
	}
	lock, err := h.lockIndex()
	if err != nil {
		return report, err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	h.index.entries = make(map[key]*CacheEntry)
	if err := h.index.flush(); err != nil {
		return report, err
	}
	if err := h.collectAllLocked(&report); err != nil {
		return report, err
	}
	return report, h.store.Cleanup()
}

func clearLegacyCache(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Name() == manifestFilename || entry.Name() == "cas" || strings.HasPrefix(entry.Name(), ".lucy-write-") {
			continue
		}
		if err := os.RemoveAll(filepath.Join(dir, entry.Name())); err != nil {
			return fmt.Errorf("remove obsolete cache data: %w", err)
		}
	}
	return nil
}
