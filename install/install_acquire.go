package install

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/artifactstore"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/github"
	"github.com/mclucy/lucy/log"
)

var (
	errOfflineArtifactUnavailable = errors.New("offline artifact unavailable")
	errArtifactDigestMismatch     = errors.New("checksum mismatch")
)

type downloadStore struct {
	offline     bool
	storeOnly   bool
	trace       *Trace
	fetch       *FetchSummary
	network     *cache.Client
	logger      *log.Logger
	githubProxy string
	injected    artifact.Materializer
	verified    sync.Map   // cas.Digest -> *objectVerification
	progressMu  sync.Mutex // concurrent staging and fetch reports
	// injectedMu keeps a caller-supplied materializer one call at a time: it
	// is external code that may keep state between calls.
	injectedMu sync.Mutex
}

// A pinned object can be requested by several staging workers. Only one
// verifies or repairs that digest at a time.
type objectVerification struct {
	mu     sync.Mutex
	info   os.FileInfo
	hashes map[string]string
}

func (s *downloadStore) verification(digest cas.Digest) *objectVerification {
	if entry, ok := s.verified.Load(digest); ok {
		return entry.(*objectVerification)
	}
	entry, _ := s.verified.LoadOrStore(digest, new(objectVerification))
	return entry.(*objectVerification)
}

func (s *downloadStore) recordVerified(digest cas.Digest, path string, hashes map[string]string) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	entry := s.verification(digest)
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.record(info, hashes)
}

func sameObject(previous, current os.FileInfo) bool {
	return previous != nil && current != nil && current.Mode().IsRegular() &&
		os.SameFile(previous, current) && previous.Size() == current.Size() &&
		previous.ModTime().Equal(current.ModTime())
}

func (entry *objectVerification) record(info os.FileInfo, hashes map[string]string) {
	if !sameObject(entry.info, info) {
		entry.info = info
		entry.hashes = maps.Clone(hashes)
	} else {
		maps.Copy(entry.hashes, hashes)
	}
}

func (entry *objectVerification) match(expected map[string]string) (map[string]string, bool) {
	if entry.hashes == nil {
		return nil, false
	}
	for algorithm, digest := range expected {
		if value, ok := entry.hashes[algorithm]; !ok || digest != "" && !strings.EqualFold(digest, value) {
			return nil, false
		}
	}
	hashes := make(map[string]string, len(expected)+1)
	hashes["sha256"] = entry.hashes["sha256"]
	for algorithm := range expected {
		hashes[algorithm] = entry.hashes[algorithm]
	}
	return hashes, true
}

func (s *downloadStore) cacheHit() {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	if s.fetch != nil {
		s.fetch.Cached++
	}
}

func (s *downloadStore) downloaded(bytes int64) {
	s.progressMu.Lock()
	defer s.progressMu.Unlock()
	if s.fetch == nil {
		return
	}
	s.fetch.Downloaded++
	s.fetch.Bytes += bytes
}

// DigestFile computes the requested digests and fails when a supplied value
// does not match. sha256 is always computed because the artifact store is
// addressed by it.
func DigestFile(filename string, algorithms map[string]string) (map[string]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return digestReader(file, filename, algorithms)
}

func digestReader(file io.Reader, filename string, algorithms map[string]string) (map[string]string, error) {
	hashers, writers, err := digestHashers(algorithms, true)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(io.MultiWriter(writers...), file); err != nil {
		return nil, err
	}
	return digestValues(hashers, algorithms, filename)
}

func digestHashers(algorithms map[string]string, includeSHA256 bool) (map[string]hash.Hash, []io.Writer, error) {
	hashers := make(map[string]hash.Hash, len(algorithms)+1)
	if includeSHA256 {
		hashers["sha256"] = sha256.New()
	}
	for algorithm := range algorithms {
		switch algorithm {
		case "sha1":
			hashers[algorithm] = sha1.New()
		case "sha512":
			hashers[algorithm] = sha512.New()
		case "md5":
			hashers[algorithm] = md5.New()
		case "sha256":
		default:
			return nil, nil, fmt.Errorf("unsupported artifact digest %s", algorithm)
		}
	}
	writers := make([]io.Writer, 0, len(hashers))
	for _, hasher := range hashers {
		writers = append(writers, hasher)
	}
	return hashers, writers, nil
}

func digestValues(hashers map[string]hash.Hash, algorithms map[string]string, filename string) (map[string]string, error) {
	values := make(map[string]string, len(hashers))
	for algorithm, hasher := range hashers {
		values[algorithm] = hex.EncodeToString(hasher.Sum(nil))
		if expected := algorithms[algorithm]; expected != "" && values[algorithm] != strings.ToLower(expected) {
			return nil, fmt.Errorf("%s %w for %s", algorithm, errArtifactDigestMismatch, filename)
		}
	}
	return values, nil
}

// materialize returns the verified store object and the digests computed while
// verifying it. The artifact is an input only; callers attach the returned
// hashes when the artifact becomes part of a lock. A store hit performs no
// network or cache access; a miss copies verified bytes from the network cache
// or the network into the store.
func (s *downloadStore) materialize(
	ctx context.Context,
	a *catalog.Artifact,
) (artifact.MaterializedFile, error) {
	return s.materializeFor(ctx, a, a.Filename)
}

func (s *downloadStore) materializeFor(
	ctx context.Context,
	a *catalog.Artifact,
	transferName string,
) (artifact.MaterializedFile, error) {
	if err := ctx.Err(); err != nil {
		return artifact.MaterializedFile{}, err
	}
	if a.Embedded != "" {
		return artifact.MaterializedFile{}, fmt.Errorf("embedded artifacts are extracted from their pinned parent")
	}
	if a.Filename == "" || filepath.Base(a.Filename) != a.Filename || strings.ContainsAny(a.Filename, "/\\") {
		return artifact.MaterializedFile{}, fmt.Errorf("invalid artifact filename %q", a.Filename)
	}
	if s.injected != nil {
		file, err := s.injectedMaterialize(ctx, a)
		if s.storeOnly && errors.Is(err, os.ErrNotExist) {
			err = fmt.Errorf("%w: %s: %w", errOfflineArtifactUnavailable, a.Filename, err)
		}
		return file, err
	}
	store, err := artifactstore.Open()
	if err != nil {
		return artifact.MaterializedFile{}, err
	}
	defer store.Close()
	var entry *objectVerification
	if expected := strings.ToLower(strings.TrimSpace(a.Hashes["sha256"])); expected != "" {
		digest, err := cas.ParseDigest("sha256:" + expected)
		if err != nil {
			return artifact.MaterializedFile{}, fmt.Errorf(
				"artifact %s has an invalid sha256 digest %q: %w",
				a.Filename, expected, err,
			)
		}
		entry = s.verification(digest)
		entry.mu.Lock()
		defer entry.mu.Unlock()
		if err := ctx.Err(); err != nil {
			return artifact.MaterializedFile{}, err
		}
		destination := store.Path(digest)
		var hashes map[string]string
		info, err := os.Lstat(destination)
		if err == nil && info.Mode().IsRegular() {
			if sameObject(entry.info, info) {
				if hashes, ok := entry.match(a.Hashes); ok {
					s.cacheHit()
					return artifact.MaterializedFile{Path: destination, Hashes: hashes}, nil
				}
			}
			hashes, err = DigestFile(destination, a.Hashes)
			if err == nil {
				fresh, statErr := os.Lstat(destination)
				if statErr != nil || !sameObject(info, fresh) {
					err = fmt.Errorf("store object changed during verification: %s", a.Filename)
				}
			}
		} else if err == nil {
			err = fmt.Errorf("store object is not a regular file: %s", a.Filename)
		}
		if err == nil {
			entry.record(info, hashes)
			s.cacheHit()
			s.logger.Debug(
				"artifact verified",
				"file", a.Filename,
				"sha256", hashes["sha256"],
				"source", "store",
			)
			return artifact.MaterializedFile{Path: destination, Hashes: hashes}, nil
		}
		if !errors.Is(err, os.ErrNotExist) {
			if s.storeOnly {
				return artifact.MaterializedFile{}, err
			}
			s.logger.Debug(
				"store object is unusable; acquiring again",
				"file", a.Filename,
				"error", err,
			)
		}
	}
	if s.storeOnly {
		return artifact.MaterializedFile{}, fmt.Errorf("%w: %s", errOfflineArtifactUnavailable, a.Filename)
	}
	source, cleanup, err := s.downloadArtifact(ctx, a, transferName)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		return artifact.MaterializedFile{}, err
	}
	object, hashes, err := importFile(store.Store, source, a.Filename, a.Hashes)
	if err != nil {
		return artifact.MaterializedFile{}, err
	}
	destination := store.Path(object.Digest)
	if entry == nil {
		s.recordVerified(object.Digest, destination, hashes)
	} else if info, statErr := os.Lstat(destination); statErr == nil && info.Mode().IsRegular() {
		entry.record(info, hashes)
	}
	s.logger.Debug(
		"artifact verified",
		"file", a.Filename,
		"sha256", hashes["sha256"],
	)
	return artifact.MaterializedFile{Path: destination, Hashes: hashes}, nil
}

// injectedMaterialize verifies bytes another operation supplied. Digests are
// recomputed from the returned file, so an injected materializer cannot lower
// the hash guarantee. Verified bytes enter the artifact store because a lock
// that mentions them must remain replayable. It does not touch the fetch
// summary, which describes the configured cache's transfers.
func (s *downloadStore) injectedMaterialize(
	ctx context.Context,
	a *catalog.Artifact,
) (artifact.MaterializedFile, error) {
	expected := maps.Clone(a.Hashes)
	input := *a
	input.Hashes = maps.Clone(a.Hashes)
	input.Modules = slices.Clone(a.Modules)
	for i := range input.Modules {
		input.Modules[i].Provides = slices.Clone(a.Modules[i].Provides)
		input.Modules[i].Dependencies = slices.Clone(a.Modules[i].Dependencies)
	}
	s.injectedMu.Lock()
	materialized, err := s.injected(ctx, &input)
	s.injectedMu.Unlock()
	if err != nil {
		return artifact.MaterializedFile{}, err
	}
	if materialized.Path == "" {
		return artifact.MaterializedFile{}, fmt.Errorf("injected artifact %s has no path", a.Filename)
	}
	store, err := artifactstore.Open()
	if err != nil {
		return artifact.MaterializedFile{}, err
	}
	defer store.Close()
	object, hashes, err := importFile(store.Store, materialized.Path, a.Filename, expected)
	if err != nil {
		return artifact.MaterializedFile{}, err
	}
	s.recordVerified(object.Digest, store.Path(object.Digest), hashes)
	return artifact.MaterializedFile{Path: store.Path(object.Digest), Hashes: hashes}, nil
}

// importFile copies a verified local file into the store while checking every
// supplied digest. A mismatch aborts the stage, so unusable bytes are never
// published. Any previous object at the same address is replaced by these
// verified bytes.
func importFile(
	store *cas.Store,
	filename, display string,
	expected map[string]string,
) (object cas.Object, hashes map[string]string, resultErr error) {
	file, err := os.Open(filename)
	if err != nil {
		return cas.Object{}, nil, err
	}
	defer file.Close()
	hashers, writers, err := digestHashers(expected, false)
	if err != nil {
		return cas.Object{}, nil, err
	}
	var pinned *cas.Digest
	if value := expected["sha256"]; value != "" {
		digest, err := cas.ParseDigest("sha256:" + strings.ToLower(value))
		if err != nil {
			return cas.Object{}, nil, err
		}
		pinned = &digest
	}
	writer, err := store.NewWriter()
	if err != nil {
		return cas.Object{}, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Abort()) }()
	drains := append(writers, writer)
	if _, err := io.Copy(io.MultiWriter(drains...), file); err != nil {
		return cas.Object{}, nil, fmt.Errorf("ingest artifact: %w", err)
	}
	hashes, err = digestValues(hashers, expected, display)
	if err != nil {
		return cas.Object{}, nil, err
	}
	object, err = writer.Commit(pinned)
	if err != nil {
		if errors.Is(err, cas.ErrCorrupt) {
			err = fmt.Errorf("sha256 checksum mismatch for %s: %w", display, err)
		}
		return cas.Object{}, nil, err
	}
	hashes["sha256"] = object.Digest.Hex()
	return object, hashes, nil
}

// artifactCacheIdentityHost is a reserved host (RFC 2606 .invalid) that only
// carries cache identities; no request targets it.
const artifactCacheIdentityHost = "artifact.cache.lucy.invalid"

// artifactCacheIdentity returns the network cache key for an artifact. Spiget
// download URLs are mutable and publish no upstream hash, so their key hashes
// the URL and adds provider, project, release, and file: neither a different
// release nor a different URL can alias cached bytes. Other providers keep the
// URL key.
func artifactCacheIdentity(a *catalog.Artifact) (string, error) {
	if a.Provider != catalog.ProviderSpiget {
		return a.URL, nil
	}
	project := strings.TrimSpace(a.ProjectID)
	release := strings.TrimSpace(a.ReleaseID)
	file := strings.TrimSpace(a.FileID)
	if project == "" || release == "" || file == "" {
		return "", fmt.Errorf(
			"spiget artifact %s is missing its project, release, or file identity",
			a.Filename,
		)
	}
	urlDigest := sha256.Sum256([]byte(a.URL))
	return "https://" + artifactCacheIdentityHost + "/" +
		url.PathEscape(a.Provider.String()) + "/" +
		hex.EncodeToString(urlDigest[:]) + "/" +
		url.PathEscape(project) + "/" +
		url.PathEscape(release) + "/" +
		url.PathEscape(file), nil
}

// downloadArtifact obtains candidate bytes from the network cache or, in
// offline mode, the cache alone. The cache identity is separate from the
// request URL, so a mutable provider URL cannot alias releases; the request
// and the persisted artifact URL stay the artifact's own URL. The returned
// path is temporary; callers import it into the store before removing the
// directory.
func (s *downloadStore) downloadArtifact(
	ctx context.Context,
	a *catalog.Artifact,
	transferName string,
) (string, func(), error) {
	if s.network == nil {
		return "", nil, errors.New("artifact download client is unavailable")
	}
	identity, err := artifactCacheIdentity(a)
	if err != nil {
		return "", nil, err
	}
	directory, err := os.MkdirTemp("", "lucy-artifact-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(directory) }
	if s.offline {
		hit, cached, _, err := s.network.Get(identity)
		if err != nil {
			return "", cleanup, err
		}
		if !hit || cached == nil {
			return "", cleanup, fmt.Errorf("offline artifact unavailable: %s", a.URL)
		}
		destination := filepath.Join(directory, a.Filename)
		out, err := os.Create(destination)
		if err != nil {
			return "", cleanup, errors.Join(err, cached.Close())
		}
		_, copyErr := io.Copy(out, cached)
		closeErr := out.Close()
		cachedErr := cached.Close()
		if copyErr != nil {
			return "", cleanup, copyErr
		}
		if closeErr != nil {
			return "", cleanup, closeErr
		}
		if cachedErr != nil {
			return "", cleanup, cachedErr
		}
		s.cacheHit()
		return destination, cleanup, nil
	}
	var read int64
	var total int64
	options := cache.DownloadOptions{
		Kind:       cache.KindArtifact,
		Filename:   a.Filename,
		RequestURL: github.ProxiedDownloadURL(a.URL, s.githubProxy),
		FileMode:   0o600,
	}
	options.WrapReader = func(reader io.Reader, size int64) io.Reader {
		total = size
		s.progressMu.Lock()
		s.trace.fetchStarted(transferName, size)
		s.progressMu.Unlock()
		return &fetchReader{
			reader: reader,
			name:   transferName,
			total:  size,
			read:   &read,
			store:  s,
		}
	}
	s.logger.Debug(
		"artifact fetch",
		"file", a.Filename,
		"host", downloadHost(options.RequestURL),
	)
	started := time.Now()
	result, err := s.network.CachedDownload(ctx, identity, directory, options)
	if err != nil {
		return "", cleanup, err
	}
	source := result.File.Name()
	if err := result.File.Close(); err != nil {
		return "", cleanup, err
	}
	origin := "network"
	if result.CacheHit {
		origin = "cache"
		s.cacheHit()
	} else {
		s.downloaded(read)
		s.progressMu.Lock()
		s.trace.fetchProgress(transferName, read, total)
		s.trace.fetchDone(transferName, false)
		s.progressMu.Unlock()
	}
	s.logger.Debug(
		"artifact fetched",
		"file", a.Filename,
		"source", origin,
		"bytes", read,
		"duration", time.Since(started).Round(time.Millisecond),
	)
	return source, cleanup, nil
}

// downloadHost names the host a download goes to, so a slow fetch points at
// the endpoint rather than at the resolver.
func downloadHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return parsed.Host
}

// fetchReader reports download byte progress through the operation trace.
type fetchReader struct {
	reader io.Reader
	name   string
	total  int64
	read   *int64
	store  *downloadStore
}

func (r *fetchReader) Read(buffer []byte) (int, error) {
	count, err := r.reader.Read(buffer)
	*r.read += int64(count)
	r.store.progressMu.Lock()
	r.store.trace.fetchProgress(r.name, *r.read, r.total)
	r.store.progressMu.Unlock()
	return count, err
}
