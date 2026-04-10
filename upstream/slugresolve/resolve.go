package slugresolve

import (
	"sync"

	"github.com/mclucy/lucy/slugmap"
	"github.com/mclucy/lucy/types"
)

// HashLookupFunc is a function that looks up a slug by file hash.
type HashLookupFunc func(filePath, urlHint string) (slug string, err error)

var (
	hashLookupMu sync.RWMutex
	hashLookups  = make(map[types.Source]HashLookupFunc)
)

// RegisterHashLookup registers a hash lookup function for a source.
// This is called by provider packages to avoid circular imports.
func RegisterHashLookup(src types.Source, fn HashLookupFunc) {
	hashLookupMu.Lock()
	defer hashLookupMu.Unlock()
	hashLookups[src] = fn
}

// ResolveSlug returns the canonical upstream slug for a locally-identified
// mod. It runs the following pipeline and short-circuits on first success:
//
//  1. Cached mapping (slugmap) — only contains hash-verified entries
//  2. File hash fingerprint (requires filePath != "")
//     - metadataURLs are used as hints: if a URL yields a candidate slug,
//     that slug is tried first in the hash lookup to avoid a full scan.
//     The URL slug itself is never persisted.
//  3. Fallback: return localId unchanged (not persisted)
//
// Only step 2 writes to slugmap.
func ResolveSlug(
	src types.Source,
	localId string,
	filePath string,
	metadataURLs []string,
) string {
	// 1. Cache hit (hash-verified)
	if slug, ok := slugmap.Default().Get(src, localId); ok {
		return slug
	}

	// 2. Hash fingerprint
	if filePath != "" {
		// Extract URL hint to try as candidate slug first (avoids full scan).
		var urlHint string
		for _, u := range metadataURLs {
			urlSrc, s, ok := ExtractFromURL(u)
			if ok && urlSrc == src && s != "" {
				urlHint = s
				break
			}
		}

		hashLookupMu.RLock()
		fn := hashLookups[src]
		hashLookupMu.RUnlock()

		if fn != nil {
			slug, err := fn(filePath, urlHint)
			if err == nil && slug != "" {
				slugmap.Default().Set(src, localId, slug, "hash")
				return slug
			}
		}
	}

	// 3. Fallback — not persisted
	return localId
}
