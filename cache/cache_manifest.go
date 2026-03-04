package cache

import (
	"errors"
	"fmt"
	"os"
	"path"
	"time"

	"github.com/mclucy/lucy/logger"
)

const (
	manifestFilename = "cache.json"
)

type cacheItem struct {
	Filename   string    `json:"filename"`
	Size       int       `json:"size"`
	Hash       string    `json:"hash"`
	Expiration time.Time `json:"expiration"`
	Key        key       `json:"key"`
}

type key string

type manifest struct {
	LifeTime time.Duration     `json:"life_time"`
	MaxSize  int               `json:"max_size"`
	Content  map[key]cacheItem `json:"content"`
}

func resetCache(manifestPath string) error {
	err := os.Remove(manifestPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}

	cacheDir := path.Dir(manifestPath)

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.Name() == manifestFilename {
			continue
		}
		entryPath := path.Join(cacheDir, entry.Name())
		if err := os.RemoveAll(entryPath); err != nil {
			logger.Warn(
				fmt.Errorf(
					"failed to remove cache item %s: %w",
					entryPath, err,
				),
			)
		}
	}

	return nil
}
