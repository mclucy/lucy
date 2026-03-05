package util

import (
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"os"
	"path"
	"time"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
)

type DownloadOptions struct {
	Kind          cache.EntryKind
	ExpectedHash  string
	HashAlgorithm cache.HashAlgorithm
	Filename      string
	WrapReader    func(io.Reader, int64) io.Reader
	OnCacheHit    func()
	TTL           time.Duration
}

type DownloadResult struct {
	File     *os.File
	CacheHit bool
	Verified bool
}

// CachedDownload downloads a file from url into dir, using the cache for
// deduplication. On cache hit the file is copied from the store and
// OnCacheHit (if set) is called. On miss the response body is streamed
// through an optional WrapReader (for progress tracking) and simultaneously
// hashed for both content-addressing and integrity verification.
func CachedDownload(url, dir string, opts DownloadOptions) (
	*DownloadResult,
	error,
) {
	hit, cachedFile, err := cache.Network().Get(url)
	if err != nil {
		logger.Warn(
			fmt.Errorf(
				"cache lookup failed, proceeding with download: %w",
				err,
			),
		)
	}
	if hit && cachedFile != nil {
		defer cachedFile.Close()
		if opts.OnCacheHit != nil {
			opts.OnCacheHit()
		}
		destPath := path.Join(dir, path.Base(cachedFile.Name()))
		destFile, err := tools.CopyFile(cachedFile, destPath)
		if err != nil {
			return nil, fmt.Errorf(
				"failed to copy cached file to destination: %w",
				err,
			)
		}
		return &DownloadResult{
			File:     destFile,
			CacheHit: true,
			Verified: false,
		}, nil
	}

	return downloadAndCache(url, dir, opts)
}

func downloadAndCache(url, dir string, opts DownloadOptions) (
	*DownloadResult,
	error,
) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download failed: status %d", resp.StatusCode)
	}

	filename := opts.Filename
	if filename == "" {
		filename = speculateFilename(resp)
	}

	tmpFile, err := os.CreateTemp("", "lucy-download-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() {
		tmpFile.Close()
		os.Remove(tmpPath)
	}()

	contentHasher := sha256.New()
	writers := []io.Writer{tmpFile, contentHasher}

	var integrityHasher hash.Hash
	if opts.ExpectedHash != "" && opts.HashAlgorithm != cache.HashNone {
		integrityHasher = newHasher(opts.HashAlgorithm)
		if integrityHasher != nil {
			writers = append(writers, integrityHasher)
		}
	}

	w := io.MultiWriter(writers...)

	var reader io.Reader = resp.Body
	if opts.WrapReader != nil {
		reader = opts.WrapReader(reader, resp.ContentLength)
	}

	size, err := io.Copy(w, reader)
	if err != nil {
		return nil, fmt.Errorf("download stream failed: %w", err)
	}

	contentHash := hex.EncodeToString(contentHasher.Sum(nil))

	integrity := cache.Integrity{State: cache.IntegrityUnverified}
	verified := false

	if integrityHasher != nil && opts.ExpectedHash != "" {
		actualHex := hex.EncodeToString(integrityHasher.Sum(nil))
		if actualHex != opts.ExpectedHash {
			return nil, fmt.Errorf(
				"integrity verification failed (%s): expected %s, got %s",
				opts.HashAlgorithm, opts.ExpectedHash, actualHex,
			)
		}
		integrity = cache.Integrity{
			Algorithm: opts.HashAlgorithm,
			Expected:  opts.ExpectedHash,
			Actual:    actualHex,
			State:     cache.IntegrityVerified,
		}
		verified = true
		logger.Debug(
			fmt.Sprintf(
				"integrity verified (%s): %s",
				opts.HashAlgorithm,
				url,
			),
		)
	}

	if filename == "" {
		filename = contentHash
	}

	destPath := path.Join(dir, filename)
	tmpFile.Close()

	src, err := os.Open(tmpPath)
	if err != nil {
		return nil, fmt.Errorf("failed to reopen temp file: %w", err)
	}
	defer src.Close()

	destFile, err := tools.CopyFile(src, destPath)
	if err != nil {
		return nil, fmt.Errorf("failed to write file to destination: %w", err)
	}

	ttl := cache.DefaultCacheConfig().DownloadKeepFor
	if opts.Kind == cache.KindMetadata {
		ttl = cache.DefaultCacheConfig().IndexRefreshAfter
	}
	if opts.TTL > 0 {
		ttl = opts.TTL
	}

	if err := cache.Network().IngestEntry(
		tmpPath, filename, url, size, contentHash,
		opts.Kind, integrity, ttl,
	); err != nil {
		logger.Warn(fmt.Errorf("failed to cache downloaded file: %w", err))
	}

	return &DownloadResult{
		File:     destFile,
		CacheHit: false,
		Verified: verified,
	}, nil
}

func newHasher(algo cache.HashAlgorithm) hash.Hash {
	switch algo {
	case cache.HashSHA1:
		return sha1.New()
	case cache.HashSHA256:
		return sha256.New()
	case cache.HashSHA512:
		return sha512.New()
	default:
		return nil
	}
}
