package cache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/log"
)

var ErrDisabled = errors.New("cache is disabled")

// missingObjectRetries bounds how often get reloads the index after the
// object of an entry vanished.
const missingObjectRetries = 2

type handler struct {
	mu     sync.Mutex
	on     bool
	dir    string
	store  *cas.Store
	index  *index
	policy Policy
	logger *log.Logger
}

func newHandler(root string, cfg Config, logger *log.Logger) *handler {
	h := &handler{
		on:     cfg.Enabled,
		dir:    root,
		policy: cfg.toPolicy(),
		logger: logger,
	}
	if !h.on {
		return h
	}
	if err := h.initialize(cfg); err != nil {
		logger.Warn(fmt.Errorf("disable cache at %s: %w", root, err))
		h.on = false
		if h.store != nil {
			if err := h.store.Close(); err != nil {
				logger.Warn(err)
			}
			h.store = nil
		}
	}
	return h
}

func (h *handler) initialize(cfg Config) (resultErr error) {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := fsutil.MkdirAllPrivate(h.dir); err != nil {
		return err
	}
	lock, err := h.acquireIndexLock()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	h.store, err = cas.New(filepath.Join(h.dir, "cas"))
	if err != nil {
		return err
	}
	h.index = newIndex(filepath.Join(h.dir, manifestFilename))
	if _, err = h.index.load(); err != nil {
		return err
	}
	removed, _, err := h.maintainLocked(nil)
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

func (h *handler) acquireIndexLock() (*fsutil.FileLock, error) {
	start := h.logger.WaitStart()
	lock, err := fsutil.LockFile(h.dir + ".lock")
	h.logger.WaitDone(log.WaitFileLock, start)
	return lock, err
}

func (h *handler) lockIndex() (*fsutil.FileLock, error) {
	if !h.on {
		return nil, ErrDisabled
	}
	lock, err := h.acquireIndexLock()
	if err != nil {
		return nil, err
	}
	reset, err := h.index.load()
	if err == nil && reset {
		err = h.collectAllLocked(nil)
	}
	if err != nil {
		return nil, errors.Join(err, lock.Close())
	}
	return lock, nil
}

// refresh reloads the index only when another process published a change.
// A read that finds the published identity unchanged costs one stat and no
// file lock, because this process holds the same entries it last loaded.
func (h *handler) refresh() (resultErr error) {
	if !h.on {
		return ErrDisabled
	}
	changed, err := h.index.changed()
	if err != nil || !changed {
		return err
	}
	lock, err := h.acquireIndexLock()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	reset, err := h.index.load()
	if err != nil {
		return err
	}
	if reset {
		return h.collectAllLocked(nil)
	}
	return nil
}

func (h *handler) newWriter() (*cas.Writer, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.on {
		return nil, ErrDisabled
	}
	return h.store.NewWriter()
}

func (h *handler) commitEntryContext(ctx context.Context, writer *cas.Writer, filename, k string, kind EntryKind, integrity Integrity, expiration time.Duration) (resultErr error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	lock, err := h.lockIndex()
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, lock.Close()) }()
	if err := ctx.Err(); err != nil {
		return err
	}
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
	h.index.put(ckey, &Entry{
		Kind:        kind,
		Filename:    sanitizeFilename(filename, object.Digest.Hex()),
		Size:        object.Size,
		ContentHash: contentHash,
		Integrity:   integrity,
		Expiration:  now.Add(expiration),
		Key:         string(ckey),
		CreatedAt:   created,
	})
	removed, dropped, err := h.maintainLocked(removed)
	if err != nil {
		return err
	}
	// Publish the entry and the maintenance deletions with one append. The
	// new entry may itself be the one maintenance dropped.
	records := make([]journalRecord, 0, len(dropped)+1)
	if entry, ok := h.index.get(ckey); ok {
		records = append(records, journalRecord{
			Op:    journalOpPut,
			Key:   ckey,
			Entry: entry,
		})
	}
	for _, k := range dropped {
		records = append(records, journalRecord{Op: journalOpDelete, Key: k})
	}
	if err := h.index.publishRecords(records); err != nil {
		return err
	}
	return h.collectCandidatesLocked(removed)
}

func (h *handler) flush() (resultErr error) {
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

func (h *handler) exist(k string) bool {
	hit, reader, _, _, err := h.get(k)
	if err != nil {
		h.logger.Warn(err)
		return false
	}
	if reader != nil {
		if err := reader.Close(); err != nil {
			h.logger.Warn(err)
			return false
		}
	}
	return hit
}

func (h *handler) get(k string) (
	hit bool,
	reader io.ReadCloser,
	filename string,
	digest cas.Digest,
	resultErr error,
) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.on {
		return false, nil, "", cas.Digest{}, nil
	}
	if err := h.refresh(); err != nil {
		return false, nil, "", cas.Digest{}, err
	}
	defer func() {
		if resultErr != nil && reader != nil {
			resultErr = errors.Join(resultErr, reader.Close())
			hit, reader, filename, digest = false, nil, "", cas.Digest{}
		}
	}()
	var err error
	for attempt := 0; ; attempt++ {
		entry, ok := h.index.get(canonicalizeKey(k))
		if !ok || !entry.Expiration.After(time.Now()) {
			return false, nil, "", cas.Digest{}, nil
		}
		digest, err = cas.ParseDigest(entry.ContentHash)
		if err != nil {
			return false, nil, "", cas.Digest{}, err
		}
		reader, err = h.store.Open(digest)
		if err == nil {
			return true, reader, entry.Filename, digest, nil
		}
		// Another process publishes a replacement entry before it reclaims
		// the old object, so a refresh after a missing object finds it.
		if !errors.Is(err, os.ErrNotExist) || attempt == missingObjectRetries {
			return false, nil, "", cas.Digest{}, err
		}
		if err := h.refresh(); err != nil {
			return false, nil, "", cas.Digest{}, err
		}
	}
}

func (h *handler) getBytes(k string) (bool, []byte, error) {
	hit, data, _, err := h.getBytesContext(context.Background(), k)
	return hit, data, err
}

func (h *handler) getBytesContext(ctx context.Context, k string) (
	hit bool,
	data []byte,
	digest cas.Digest,
	resultErr error,
) {
	hit, reader, _, digest, err := h.get(k)
	if err != nil || !hit {
		return false, nil, cas.Digest{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, reader.Close()) }()
	data, err = io.ReadAll(contextReader{ctx, reader})
	if err != nil {
		return false, nil, cas.Digest{}, err
	}
	return true, data, digest, nil
}

func (h *handler) remove(k string) (resultErr error) {
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
	if err := h.index.publishRecords([]journalRecord{{
		Op:  journalOpDelete,
		Key: canonicalizeKey(k),
	}}); err != nil {
		return err
	}
	return h.collectCandidatesLocked([]cas.Digest{digest})
}

func (h *handler) all() []*Entry {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.on {
		return nil
	}
	if err := h.refresh(); err != nil {
		h.logger.Warn(err)
		return nil
	}
	keys := slices.Sorted(maps.Keys(h.index.entries))
	entries := make([]*Entry, 0, len(keys))
	for _, k := range keys {
		entry := *h.index.entries[k]
		entries = append(entries, &entry)
	}
	return entries
}

func (h *handler) clearAll() (report ResetReport, resultErr error) {
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
	h.index.entries = make(map[key]*Entry)
	if err := h.index.flush(); err != nil {
		return report, err
	}
	if err := h.collectAllLocked(&report); err != nil {
		return report, err
	}
	return report, h.store.Cleanup()
}
