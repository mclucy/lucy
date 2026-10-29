package cache

import (
	"cmp"
	"maps"
	"net/url"
	"path"
	"slices"
	"strings"
	"time"
)

func expiredEntries(entries map[key]*Entry, now time.Time) []key {
	var expired []key
	for k, entry := range entries {
		if entry.Expiration.Before(now) {
			expired = append(expired, k)
		}
	}
	return expired
}

type evictionTarget struct {
	key key
	exp time.Time
}

func evictionCandidates(
	entries map[key]*Entry,
	policy Policy,
) []evictionTarget {
	type objectKey struct {
		kind EntryKind
		hash string
	}
	totals := map[EntryKind]int64{}
	references := make(map[objectKey]int, len(entries))
	targets := make([]evictionTarget, 0, len(entries))
	for k, entry := range entries {
		object := objectKey{kind: entry.Kind, hash: entry.ContentHash}
		if references[object] == 0 {
			totals[entry.Kind] += entry.Size
		}
		references[object]++
		targets = append(targets, evictionTarget{key: k, exp: entry.Expiration})
	}
	slices.SortFunc(targets, func(a, b evictionTarget) int {
		if c := a.exp.Compare(b.exp); c != 0 {
			return c
		}
		return cmp.Compare(a.key, b.key)
	})
	var result []evictionTarget
	for _, target := range targets {
		entry := entries[target.key]
		if totals[entry.Kind] <= policy.ConfigFor(entry.Kind).MaxSize {
			continue
		}
		result = append(result, target)
		object := objectKey{kind: entry.Kind, hash: entry.ContentHash}
		references[object]--
		if references[object] == 0 {
			totals[entry.Kind] -= entry.Size
		}
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
		keys := slices.Sorted(maps.Keys(params))
		var parts []string
		for _, k := range keys {
			vals := slices.Clone(params[k])
			slices.Sort(vals)
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
