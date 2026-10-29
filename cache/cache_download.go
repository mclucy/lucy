package cache

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/internal/fsutil"
)

// DownloadOptions controls artifact caching and downloading. The URL passed to
// CachedDownload is the cache identity; RequestURL, when set, is used only for
// the network request.
type DownloadOptions struct {
	Kind               EntryKind
	ExpectedHash       string
	HashAlgorithm      HashAlgorithm
	Filename           string
	RequestURL         string
	WrapReader         func(io.Reader, int64) io.Reader
	OnCacheHit         func()
	OnResolvedFilename func(string)
	TTL                time.Duration
	FileMode           os.FileMode
}

type BytesRequestOptions struct {
	Kind          EntryKind
	Headers       http.Header
	ExpectedHash  string
	HashAlgorithm HashAlgorithm
	TTL           time.Duration
	MaxBytes      int64
}

type BytesResponse struct {
	StatusCode int
	Data       []byte
	CacheHit   bool
}

type DownloadResult struct {
	File     *os.File
	CacheHit bool
	Verified bool
}

// CachedDownload downloads a file from url into dir, using the cache for
// deduplication. On cache hit the indexed bytes are copied atomically into
// dir and OnCacheHit (if set) is called. On miss the response body is
// streamed once through an optional WrapReader (for progress tracking) into
// a cache stage, verified, and only then written to the destination and
// published to the cache.
func (c *Client) CachedDownload(
	ctx context.Context,
	url, dir string,
	opts DownloadOptions,
) (*DownloadResult, error) {
	c.lifetime.RLock()
	defer c.lifetime.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateDownloadURL(url); err != nil {
		return nil, err
	}
	requestURL := url
	if opts.RequestURL != "" {
		requestURL = opts.RequestURL
	}
	if err := validateDownloadURL(requestURL); err != nil {
		return nil, err
	}
	if opts.FileMode == 0 {
		opts.FileMode = 0o640
	}
	hit, cachedReader, cachedFilename, err := c.cache().get(url)
	if err != nil {
		c.logger.Warn(
			fmt.Errorf(
				"cache lookup failed, proceeding with download: %w",
				err,
			),
		)
	}
	if hit && cachedReader != nil {
		result, copyErr := copyCachedFile(
			ctx,
			cachedReader,
			cachedFilename,
			dir,
			opts,
		)
		closeErr := cachedReader.Close()
		if copyErr == nil {
			if closeErr != nil {
				return nil, errors.Join(closeErr, result.File.Close())
			}
			return result, nil
		}
		copyErr = errors.Join(copyErr, closeErr)
		if !errors.Is(copyErr, cas.ErrCorrupt) &&
			!errors.Is(copyErr, cas.ErrRead) {
			return nil, copyErr
		}
		c.logger.Warn(
			fmt.Errorf(
				"cached contents unreadable, refetching %s: %w",
				url,
				copyErr,
			),
		)
	}

	return c.downloadAndCache(ctx, url, requestURL, dir, opts)
}

// copyCachedFile materializes a cache hit under dir. cas.ErrCorrupt and
// cas.ErrRead mean the stored bytes are unusable and the caller must treat
// the hit as a miss; the atomic copy leaves the destination untouched.
func copyCachedFile(
	ctx context.Context,
	reader io.Reader,
	cachedFilename string,
	dir string,
	opts DownloadOptions,
) (*DownloadResult, error) {
	resolvedName := sanitizeFilename(cachedFilename, "artifact")
	if opts.OnResolvedFilename != nil {
		opts.OnResolvedFilename(resolvedName)
	}
	destPath := filepath.Join(dir, resolvedName)
	if !containedUnder(dir, destPath) {
		return nil, fmt.Errorf(
			"cached filename %q escapes destination directory",
			resolvedName,
		)
	}
	destFile, err := copyFileContext(ctx, reader, destPath, opts.FileMode)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to copy cached file to destination: %w",
			err,
		)
	}
	if opts.OnCacheHit != nil {
		opts.OnCacheHit()
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, destFile.Close())
	}
	return &DownloadResult{
		File:     destFile,
		CacheHit: true,
		Verified: false,
	}, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if ctxErr := r.ctx.Err(); ctxErr != nil {
		return n, ctxErr
	}
	return n, err
}

func copyFileContext(ctx context.Context, reader io.Reader, path string, mode os.FileMode) (file *os.File, resultErr error) {
	dir := filepath.Dir(path)
	if err := fsutil.MkdirAllPrivate(dir); err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, root.Close()) }()
	if err := fsutil.WriteRoot(root, filepath.Base(path), contextReader{ctx, reader}, fsutil.WriteOptions{
		Mode: mode, Replace: true, Check: ctx.Err,
	}); err != nil {
		return nil, err
	}
	return os.Open(path)
}

// CachedGetBytes fetches bytes from url, using the cache for deduplication.
// On cache hit the bytes are returned directly. On miss the response body is
// read into memory with a size limit, staged for content addressing, verified
// against the optional upstream digest, and then cached.
func (c *Client) CachedGetBytes(
	ctx context.Context,
	url string,
	opts BytesRequestOptions,
) ([]byte, error) {
	res, err := c.CachedGetRequest(ctx, url, opts)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch failed: status %d", res.StatusCode)
	}
	return res.Data, nil
}

// CachedGetRequest fetches bytes from url and returns the HTTP status code
// separately from transport/read/cache errors. Non-2xx responses are not
// cached.
func (c *Client) CachedGetRequest(
	ctx context.Context,
	url string,
	opts BytesRequestOptions,
) (*BytesResponse, error) {
	c.lifetime.RLock()
	defer c.lifetime.RUnlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateDownloadURL(url); err != nil {
		return nil, err
	}

	hit, data, err := c.cache().getBytesContext(ctx, url)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err != nil {
		c.logger.Warn(
			fmt.Errorf(
				"cache lookup failed, proceeding with fetch: %w",
				err,
			),
		)
	}
	if hit && data != nil {
		return &BytesResponse{
			StatusCode: http.StatusOK,
			Data:       data,
			CacheHit:   true,
		}, nil
	}

	maxBytes := opts.MaxBytes
	if maxBytes == 0 {
		maxBytes = 50 * 1024 * 1024
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create fetch request failed: %w", err)
	}
	for key, values := range opts.Headers {
		for _, value := range values {
			req.Header.Add(key, value)
		}
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, err := readLimitedBytes(contextReader{ctx, resp.Body}, maxBytes)
		if err != nil {
			return nil, err
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return &BytesResponse{StatusCode: resp.StatusCode, Data: body}, nil
	}

	stage := c.newByteStage()
	defer func() {
		if err := stage.Close(); err != nil {
			c.logger.Warn(fmt.Errorf("remove download stage: %w", err))
		}
	}()
	writers := []io.Writer{stage}
	integrityHasher := newIntegrityHasher(opts.HashAlgorithm, opts.ExpectedHash)
	if integrityHasher != nil {
		writers = append(writers, integrityHasher)
	}

	var bodyBuf bytes.Buffer
	writers = append(writers, &bodyBuf)
	limitedReader := io.LimitReader(contextReader{ctx, resp.Body}, maxBytes)
	if _, err := io.Copy(io.MultiWriter(writers...), limitedReader); err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}

	body := bodyBuf.Bytes()
	if int64(len(body)) >= maxBytes {
		return nil, fmt.Errorf(
			"response too large: exceeded %d bytes",
			maxBytes,
		)
	}

	object, err := stage.seal()
	if err != nil {
		return nil, err
	}
	contentDigest := object.Digest

	integrity, _, err := c.verifyIntegrity(
		integrityActual(opts.HashAlgorithm, contentDigest, integrityHasher),
		opts.HashAlgorithm,
		opts.ExpectedHash,
		url,
	)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if stage.writer != nil {
		if err := c.cache().commitEntryContext(
			ctx,
			stage.writer, "", url, opts.Kind, integrity, opts.TTL,
		); err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			c.logger.Warn(fmt.Errorf("failed to cache bytes: %w", err))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return &BytesResponse{
		StatusCode: resp.StatusCode,
		Data:       body,
		CacheHit:   false,
	}, nil
}

func readLimitedBytes(reader io.Reader, maxBytes int64) ([]byte, error) {
	bytes, err := fsutil.CopyBytes(reader, maxBytes)
	if err != nil {
		return nil, fmt.Errorf("read failed: %w", err)
	}
	return bytes, nil
}

// downloadAndCache streams requestURL into dir. The destination write and
// the cache commit happen only after the staged SHA-256 digest and any
// upstream integrity check pass, so a rejected response reaches neither.
func (c *Client) downloadAndCache(
	ctx context.Context,
	url, requestURL, dir string,
	opts DownloadOptions,
) (*DownloadResult, error) {
	if err := validateDownloadURL(url); err != nil {
		return nil, err
	}
	if err := validateDownloadURL(requestURL); err != nil {
		return nil, err
	}

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("download failed: %w", err)
	}
	resp, err := c.Do(request)
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

	stage, err := c.newDownloadStage()
	if err != nil {
		return nil, err
	}
	defer func() {
		if err := stage.Close(); err != nil {
			c.logger.Warn(fmt.Errorf("remove download stage: %w", err))
		}
	}()
	writers := []io.Writer{stage}
	integrityHasher := newIntegrityHasher(opts.HashAlgorithm, opts.ExpectedHash)
	if integrityHasher != nil {
		writers = append(writers, integrityHasher)
	}

	var reader io.Reader = resp.Body
	if opts.WrapReader != nil {
		reader = opts.WrapReader(reader, resp.ContentLength)
	}

	if _, err := io.Copy(io.MultiWriter(writers...), contextReader{ctx, reader}); err != nil {
		return nil, fmt.Errorf("download stream failed: %w", err)
	}

	object, err := stage.seal()
	if err != nil {
		return nil, err
	}
	contentDigest := object.Digest

	integrity, verified, err := c.verifyIntegrity(
		integrityActual(opts.HashAlgorithm, contentDigest, integrityHasher),
		opts.HashAlgorithm,
		opts.ExpectedHash,
		url,
	)
	if err != nil {
		return nil, err
	}

	filename = sanitizeFilename(filename, contentDigest.Hex())

	destPath := filepath.Join(dir, filename)
	if !containedUnder(dir, destPath) {
		return nil, fmt.Errorf(
			"downloaded filename %q escapes destination directory",
			filename,
		)
	}

	if err := stage.writer.Rewind(); err != nil {
		return nil, fmt.Errorf("rewind staged download: %w", err)
	}
	destFile, err := copyFileContext(ctx, stage.writer, destPath, opts.FileMode)
	if err != nil {
		return nil, fmt.Errorf("write downloaded file: %w", err)
	}

	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, destFile.Close())
	}
	if stage.cacheable {
		if err := c.cache().commitEntryContext(
			ctx,
			stage.writer, filename, url, opts.Kind, integrity, opts.TTL,
		); err != nil {
			if ctx.Err() != nil {
				return nil, errors.Join(ctx.Err(), destFile.Close())
			}
			c.logger.Warn(fmt.Errorf("failed to cache downloaded file: %w", err))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, destFile.Close())
	}

	return &DownloadResult{
		File:     destFile,
		CacheHit: false,
		Verified: verified,
	}, nil
}

// newIntegrityHasher creates the hasher for an upstream digest, or nil when
// the content SHA-256 already covers that expectation or none was supplied.
func newIntegrityHasher(algorithm HashAlgorithm, expectedHash string) hash.Hash {
	if expectedHash == "" || algorithm == HashNone || algorithm == HashSHA256 {
		return nil
	}
	switch algorithm {
	case HashSHA1:
		return sha1.New()
	case HashSHA512:
		return sha512.New()
	default:
		return nil
	}
}

// integrityActual reports the digest compared against the upstream
// expectation: the content digest for SHA-256, the separate hasher otherwise.
func integrityActual(
	algorithm HashAlgorithm,
	content cas.Digest,
	hasher hash.Hash,
) string {
	if algorithm == HashSHA256 {
		return content.Hex()
	}
	if hasher != nil {
		return hex.EncodeToString(hasher.Sum(nil))
	}
	return ""
}

func (c *Client) verifyIntegrity(
	actualHash string,
	algorithm HashAlgorithm,
	expectedHash string,
	url string,
) (Integrity, bool, error) {
	integrity := Integrity{State: IntegrityUnverified}
	verified := false

	if expectedHash != "" && actualHash != "" {
		if actualHash != expectedHash {
			return integrity, false, fmt.Errorf(
				"integrity verification failed (%s): expected %s, got %s",
				algorithm, expectedHash, actualHash,
			)
		}
		integrity = Integrity{
			Algorithm: algorithm,
			Expected:  expectedHash,
			Actual:    actualHash,
			State:     IntegrityVerified,
		}
		verified = true
		c.logger.Debug(
			fmt.Sprintf(
				"integrity verified (%s): %s",
				algorithm,
				url,
			),
		)
	}

	return integrity, verified, nil
}

func validateDownloadURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid download URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf(
			"unsupported URL scheme %q (only http/https)",
			u.Scheme,
		)
	}
	return nil
}
