package cache

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mclucy/lucy/global"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
)

// This is traditional OOP

const (
	defaultLifeTime = global.ThirtyMinutes
	maxSize         = 30 * 1024 * 1024 // 30MB
)

type handler struct {
	mu           sync.RWMutex
	on           bool
	dir          string
	manifest     *manifest
	manifestPath string
}

func newHandler(name string) (obj *handler) {
	obj = &handler{
		on:       true,
		dir:      setDir(name),
		manifest: nil,
	}
	if err := os.MkdirAll(obj.dir, 0o700); err != nil {
		logger.Warn(
			fmt.Errorf(
				"cannnot create cache directory, disabling %s cache: %w",
				name, err,
			),
		)
		obj.on = false
	}

	obj.manifestPath = path.Join(obj.dir, manifestFilename)
	obj.manifest = readManifest(obj.manifestPath)
	if obj.dir == "" || obj.manifest == nil || obj.manifest.Content == nil {
		obj.on = false
	}

	// Maintenance on initialization
	//  - clear expired cache
	//  - maintain cache limit
	//  - update manifest
	if obj.on {
		obj.clearExpiredCache()
		obj.maintainCacheLimit()
		if err := updateManifest(obj.manifestPath, obj.manifest); err != nil {
			logger.Warn(
				fmt.Errorf(
					"failed to update manifest on initialization: %w",
					err,
				),
			)
		}
	}

	return obj
}

// Add
//
// If expiration is set to 0, the default expiration time will be applied.
//
// If the cache already exists, it will be updated with the new data.
func (h *handler) Add(
	data []byte,
	filename string,
	k string,
	expiration time.Duration,
) (err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if err := h.addLocked(data, filename, k, expiration); err != nil {
		return err
	}
	if err := updateManifest(h.manifestPath, h.manifest); err != nil {
		logger.Warn(
			fmt.Errorf(
				"failed to update manifest after adding item: %w",
				err,
			),
		)
	}
	return nil
}

func (h *handler) addLocked(
	data []byte,
	filename string,
	k string,
	expiration time.Duration,
) error {
	if !h.on {
		return nil
	}
	key := canonicalizeKey(k)
	hash := hash(data)
	if filename == "" {
		filename = hash
	}

	if h.existLocked(k) {
		if h.manifest.Content[key].Hash != hash {
			if err := h.removeLocked(key); err != nil {
				return fmt.Errorf("failed to remove stale cache entry before update: %w", err)
			}
		} else {
			return nil
		}
	}

	dir := path.Join(h.dir, hash)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// sanitize filename to prevent path traversal
	filename = filepath.Base(filename)
	if filename == "." || filename == "/" || filename == string(filepath.Separator) {
		filename = hash
	}
	filePath := path.Join(dir, filename)
	// verify the resolved path is contained under the hash directory
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to resolve cache directory: %w", err)
	}
	absFile, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to resolve cache file path: %w", err)
	}
	if !strings.HasPrefix(absFile, absDir+string(filepath.Separator)) {
		return fmt.Errorf("filename %q escapes cache directory", filename)
	}
	if err := os.WriteFile(filePath, data, 0o600); err != nil {
		return err
	}

	h.manifest.Content[key] = cacheItem{
		Filename: filename,
		Size:     len(data),
		Hash:     hash,
		Expiration: tools.Ternary(
			expiration == 0,
			time.Now().Add(defaultLifeTime),
			time.Now().Add(expiration),
		),
		Key: key,
	}

	return nil
}

func (h *handler) Flush() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.on {
		return nil
	}
	return updateManifest(h.manifestPath, h.manifest)
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
	_, ok := h.manifest.Content[canonicalizeKey(k)]
	return ok
}

func (h *handler) Get(k string) (hit bool, file *os.File, err error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if !h.on {
		return false, nil, nil
	}

	key := canonicalizeKey(k)

	item, ok := h.manifest.Content[key]
	if !ok {
		return false, nil, nil
	}
	itemPath := path.Join(h.dir, item.Hash, item.Filename)
	file, err = os.Open(itemPath)
	if err != nil {
		return false, nil, err
	}
	return true, file, nil
}

func (h *handler) Remove(k string) (err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.removeLocked(canonicalizeKey(k))
}

func (h *handler) removeLocked(key key) (err error) {
	if err := h.removeEntryLocked(key); err != nil {
		return err
	}
	if err := updateManifest(h.manifestPath, h.manifest); err != nil {
		logger.Warn(
			fmt.Errorf(
				"failed to update manifest after removing item: %w",
				err,
			),
		)
	}
	return nil
}

func (h *handler) removeEntryLocked(key key) error {
	if !h.on {
		return nil
	}
	item, ok := h.manifest.Content[key]
	if !ok {
		return nil
	}
	itemPath := path.Join(h.dir, item.Hash)
	if err := os.RemoveAll(itemPath); err != nil {
		return err
	}
	delete(h.manifest.Content, key)
	return nil
}

// ClearAll clears the cache and creates a new manifest.
//
// This is useful when the cache is corrupted or when you want to start fresh.
func (h *handler) ClearAll() error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if !h.on {
		return nil
	}

	// clear the cache directory
	err := resetCache(h.manifestPath)
	if err != nil {
		return fmt.Errorf("failed to clear cache: %w", err)
	}

	// create a new manifest
	newManifest := createManifest(h.manifestPath)
	if newManifest == nil {
		return fmt.Errorf("failed to create new manifest after clearing cache")
	}

	// update the manifest
	h.manifest = newManifest

	return nil
}
