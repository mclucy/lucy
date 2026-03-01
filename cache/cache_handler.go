package cache

import (
	"fmt"
	"os"
	"path"
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
	if err := os.MkdirAll(obj.dir, os.ModePerm); err != nil {
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
	if !h.on {
		return nil
	}
	key := key(k)
	hash := hash(data)
	if filename == "" {
		filename = hash
	}

	if h.Exist(k) {
		// update the cache item if it exists
		if h.manifest.Content[key].Sha1 != hash {
			_ = h.Remove(key)
		} else {
			// same hash, no need to update
			return nil
		}
	}

	// using the hash as the directory name
	dir := path.Join(h.dir, hash)
	err = os.MkdirAll(dir, os.ModeDir|os.ModePerm)
	if err != nil {
		return err
	}
	// create and write the file
	filepath := path.Join(dir, filename)
	err = os.WriteFile(filepath, data, 0o644)
	if err != nil {
		return err
	}

	// update the manifest
	h.manifest.Content[key] = cacheItem{
		Filename: filename,
		Size:     len(data),
		Sha1:     hash,
		Expiration: tools.Ternary(
			expiration == 0,
			time.Now().Add(defaultLifeTime),
			time.Now().Add(expiration),
		),
		Key: key,
	}

	// update manifest file
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

func (h *handler) Exist(k string) bool {
	if !h.on {
		return false
	}
	_, ok := h.manifest.Content[key(k)]
	return ok
}

func (h *handler) Get(k string) (hit bool, file *os.File, err error) {
	if !h.on {
		return false, nil, nil
	}

	key := key(k)

	item, ok := h.manifest.Content[key]
	if !ok {
		return false, nil, nil
	}
	itemPath := path.Join(h.dir, item.Sha1, item.Filename)
	file, err = os.Open(itemPath)
	if err != nil {
		return false, nil, err
	}
	return true, file, nil
}

func (h *handler) Remove(key key) (err error) {
	if !h.on {
		return nil
	}
	item, ok := h.manifest.Content[key]
	if !ok {
		return nil
	}
	itemPath := path.Join(h.dir, item.Sha1)
	err = os.RemoveAll(itemPath)
	if err != nil {
		return err
	}
	delete(h.manifest.Content, key)

	// update manifest file
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

// ClearAll clears the cache and creates a new manifest.
//
// This is useful when the cache is corrupted or when you want to start fresh.
func (h *handler) ClearAll() error {
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
		h.on = false
		return fmt.Errorf("failed to create new manifest after clearing cache")
	}

	// update the manifest
	h.manifest = newManifest

	return nil
}
