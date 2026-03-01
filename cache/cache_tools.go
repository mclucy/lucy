package cache

import (
	"crypto/md5"
	"fmt"
	"os"
	"path"
	"slices"
	"time"

	"github.com/mclucy/lucy/global"
	"github.com/mclucy/lucy/logger"
)

var hash = func(data []byte) string { return fmt.Sprintf("%x", md5.Sum(data)) }

func setDir(name string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return path.Join(dir, global.ProgramName, name)
}

func (h *handler) clearExpiredCache() {
	for _, item := range h.manifest.Content {
		if item.Expiration.Before(time.Now()) {
			logger.Info("removing expired cache item " + item.Key)
			err := h.Remove(item.Key)
			if err != nil {
				continue
			}
		}
	}
}

func (h *handler) maintainCacheLimit() {
	size := 0
	arr := make([]cacheItem, 0)
	for _, item := range h.manifest.Content {
		size += item.Size
		arr = append(arr, item)
	}
	slices.SortFunc(
		arr,
		func(a, b cacheItem) int { return int(a.Expiration.Sub(b.Expiration)) },
	)
	for _, item := range arr {
		if size <= h.manifest.MaxSize {
			break
		}
		logger.Info("removing cache item " + item.Key)
		err := h.Remove(item.Key)
		if err != nil {
			continue
		}
		size -= item.Size
	}
}
