package cache

import (
	"crypto/sha256"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mclucy/lucy/log"
)

var h = func(data []byte) string {
	return fmt.Sprintf(
		"%x",
		sha256.Sum256(data),
	)
}

func setDir(name string) string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "lucy", name)
}

func (handler *handler) clearExpiredCache() {
	expired := expiredEntries(handler.index.all(), time.Now())
	for _, k := range expired {
		log.Info("removing expired cache item " + k)
		if err := handler.removeEntryLocked(k); err != nil {
			continue
		}
	}
}

// expiredEntries returns keys of all expired entries.
func expiredEntries(entries map[key]*CacheEntry, now time.Time) []key {
	var expired []key
	for k, entry := range entries {
		if entry.Expiration.Before(now) {
			expired = append(expired, k)
		}
	}
	return expired
}

func (handler *handler) maintainCacheLimit() {
	evicted := evictionCandidates(handler.index.all(), handler.policy)
	for _, e := range evicted {
		log.Info("removing cache item " + e.key)
		if err := handler.removeEntryLocked(e.key); err != nil {
			continue
		}
	}
}

type evictionTarget struct {
	key  key
	kind EntryKind
	size int64
	exp  time.Time
}

func evictionTargets(entries map[key]*CacheEntry) (
	map[EntryKind]int64,
	[]evictionTarget,
) {
	totals := map[EntryKind]int64{}
	var targets []evictionTarget
	for k, entry := range entries {
		totals[entry.Kind] += entry.Size
		targets = append(
			targets, evictionTarget{
				key:  k,
				kind: entry.Kind,
				size: entry.Size,
				exp:  entry.Expiration,
			},
		)
	}
	sort.Slice(
		targets, func(i, j int) bool {
			return targets[i].exp.Before(targets[j].exp)
		},
	)
	return totals, targets
}

func evictionCandidates(
	entries map[key]*CacheEntry,
	policy Policy,
) []evictionTarget {
	totals, targets := evictionTargets(entries)
	var result []evictionTarget
	for _, e := range targets {
		limit := policy.ConfigFor(e.kind).MaxSize
		if totals[e.kind] <= limit {
			continue
		}
		result = append(result, e)
		totals[e.kind] -= e.size
	}
	return result
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
