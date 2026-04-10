package slugresolve

import (
	"github.com/mclucy/lucy/slugmap"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/curseforge"
	"github.com/mclucy/lucy/upstream/modrinth"
)

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

		var slug string
		var err error
		switch src {
		case types.SourceModrinth:
			slug, err = modrinth.SlugFromFilePathWithHint(filePath, urlHint)
		case types.SourceCurseForge:
			slug, err = curseforge.SlugFromFilePathWithHint(filePath, urlHint)
		}
		if err == nil && slug != "" {
			slugmap.Default().Set(src, localId, slug, "hash")
			return slug
		}
	}

	// 3. Fallback — not persisted
	return localId
}
