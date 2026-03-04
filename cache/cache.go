// Package cache provides a content-addressed artifact and metadata cache for
// downloaded files. It is organized in three internal layers:
//
//   - store:  content-addressed blob IO (read/write/ingest/remove)
//   - index:  versioned manifest tracking cache entries with v1→v2 migration
//   - policy: per-kind (artifact vs metadata) TTL and size-limit enforcement
//
// The primary consumer-facing API is [util.CachedDownload], which wraps HTTP
// downloads with streaming hash verification, cache storage, and optional
// progress bar injection via DownloadOptions.WrapReader.
//
// Cache entries are keyed by URL and classified as either KindArtifact (JARs,
// binaries — long TTL) or KindMetadata (version manifests, API responses —
// short TTL). Integrity verification is performed inline during download when
// an expected hash is provided.
package cache

import "sync"

var (
	networkOnce    sync.Once
	networkHandler *handler
)

func Network() *handler {
	networkOnce.Do(func() {
		networkHandler = newHandler("network", DefaultCacheConfig())
	})
	return networkHandler
}
