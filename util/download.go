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
	Kind               cache.EntryKind
	ExpectedHash       string
	HashAlgorithm      cache.HashAlgorithm
	Filename           string
	WrapReader         func(io.Reader, int64) io.Reader
	OnCacheHit         func()
	OnResolvedFilename func(string)
	TTL                time.Duration
	FileMode           os.FileMode
}

type BytesRequestOptions struct {
	Kind          cache.EntryKind
	ExpectedHash  string
	HashAlgorithm cache.HashAlgorithm
	TTL           time.Duration
	MaxBytes      int64
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
	if opts.FileMode == 0 {
		opts.FileMode = 0o640
	}
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
		resolvedName := path.Base(cachedFile.Name())
		if opts.OnResolvedFilename != nil {
			opts.OnResolvedFilename(resolvedName)
		}
		if opts.OnCacheHit != nil {
			opts.OnCacheHit()
		}
		destPath := path.Join(dir, resolvedName)
		destFile, err := tools.CopyFile(cachedFile, destPath, opts.FileMode)
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

// CachedGetBytes fetches bytes from url, using the cache for deduplication.
// On cache hit the bytes are returned directly. On miss the response body is
// read into memory with a size limit, hashed for content-addressing and
// integrity verification, then cached.
func CachedGetBytes(url string, opts BytesRequestOptions) ([]byte, error) {
	hit, data, err := cache.Network().GetBytes(url)
	if err != nil {
		logger.Warn(
			fmt.Errorf(
				"cache lookup failed, proceeding with fetch: %w",
				err,
			),
		)
	}
	if hit && data != nil {
		return data, nil
	}

	maxBytes := opts.MaxBytes
	if maxBytes == 0 {
		maxBytes = 50 * 1024 * 1024
	}

	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch failed: status %d", resp.StatusCode)
	}

	contentHasher := sha256.New()
	limitedReader := io.LimitReader(resp.Body, maxBytes)
	writers := []io.Writer{contentHasher}

	var integrityHasher hash.Hash
	if opts.ExpectedHash != "" && opts.HashAlgorithm != cache.HashNone {
		integrityHasher = newHasher(opts.HashAlgorithm)
		if integrityHasher != nil {
			writers = append(writers, integrityHasher)
		}
	}

	w := io.MultiWriter(writers...)
	bytes, err := io.ReadAll(io.TeeReader(limitedReader, w))
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}

	if int64(len(bytes)) >= maxBytes {
		return nil, fmt.Errorf("response too large: exceeded %d bytes", maxBytes)
	}

	contentHash := hex.EncodeToString(contentHasher.Sum(nil))

	integrity, _, err := verifyIntegrity(integrityHasher, opts.HashAlgorithm, opts.ExpectedHash, url)
	if err != nil {
		return nil, err
	}

	ttl := resolveTTL(opts.Kind, opts.TTL)

	if err := cache.Network().AddEntry(
		bytes, contentHash, url, opts.Kind, integrity, ttl,
	); err != nil {
		logger.Warn(fmt.Errorf("failed to cache bytes: %w", err))
	}

	return bytes, nil
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

	if opts.OnResolvedFilename != nil && filename != "" {
		opts.OnResolvedFilename(filename)
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

	integrity, verified, err := verifyIntegrity(integrityHasher, opts.HashAlgorithm, opts.ExpectedHash, url)
	if err != nil {
		return nil, err
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

	destFile, err := tools.CopyFile(src, destPath, opts.FileMode)
	if err != nil {
		return nil, fmt.Errorf("failed to write file to destination: %w", err)
	}

	ttl := resolveTTL(opts.Kind, opts.TTL)

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

func resolveTTL(kind cache.EntryKind, customTTL time.Duration) time.Duration {
	ttl := cache.DefaultCacheConfig().DownloadKeepFor
	if kind == cache.KindMetadata {
		ttl = cache.DefaultCacheConfig().IndexRefreshAfter
	}
	if customTTL > 0 {
		ttl = customTTL
	}
	return ttl
}

func verifyIntegrity(hasher hash.Hash, algorithm cache.HashAlgorithm, expectedHash string, url string) (cache.Integrity, bool, error) {
	integrity := cache.Integrity{State: cache.IntegrityUnverified}
	verified := false

	if hasher != nil && expectedHash != "" {
		actualHex := hex.EncodeToString(hasher.Sum(nil))
		if actualHex != expectedHash {
			return integrity, false, fmt.Errorf(
				"integrity verification failed (%s): expected %s, got %s",
				algorithm, expectedHash, actualHex,
			)
		}
		integrity = cache.Integrity{
			Algorithm: algorithm,
			Expected:  expectedHash,
			Actual:    actualHex,
			State:     cache.IntegrityVerified,
		}
		verified = true
		logger.Debug(
			fmt.Sprintf(
				"integrity verified (%s): %s",
				algorithm,
				url,
			),
		)
	}

	return integrity, verified, nil
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
