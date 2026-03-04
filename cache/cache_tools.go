package cache

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"path"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/mclucy/lucy/global"
	"github.com/mclucy/lucy/logger"
)

var hash = func(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func setDir(name string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return path.Join(dir, global.ProgramName, name)
}

func (h *handler) clearExpiredCache() {
	now := time.Now()
	var expired []key
	for k, item := range h.manifest.Content {
		if item.Expiration.Before(now) {
			expired = append(expired, k)
		}
	}
	for _, k := range expired {
		logger.Info("removing expired cache item " + k)
		if err := h.removeEntryLocked(k); err != nil {
			continue
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
		func(a, b cacheItem) int { return a.Expiration.Compare(b.Expiration) },
	)
	for _, item := range arr {
		if size <= h.manifest.MaxSize {
			break
		}
		logger.Info("removing cache item " + item.Key)
		if err := h.removeEntryLocked(item.Key); err != nil {
			continue
		}
		size -= item.Size
	}
}

func canonicalizeKey(k string) key {
	u, err := url.Parse(k)
	if err != nil || u.Scheme == "" {
		return key(k)
	}

	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	host := u.Hostname()
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		u.Host = host
	}

	if u.Path != "" {
		u.Path = path.Clean(u.Path)
	}

	if u.RawQuery != "" {
		params := u.Query()
		keys := make([]string, 0, len(params))
		for k := range params {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			vals := params[k]
			sort.Strings(vals)
			for _, v := range vals {
				parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
			}
		}
		u.RawQuery = strings.Join(parts, "&")
	}

	u.Fragment = ""
	u.RawFragment = ""

	return key(u.String())
}
