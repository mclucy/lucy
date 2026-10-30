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
	"time"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/artifactstore"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/github"
	"github.com/mclucy/lucy/log"
)

type downloadStore struct {
	ctx         context.Context
	offline     bool
	trace       *Trace
	fetch       *FetchSummary
	network     *cache.Client
	logger      *log.Logger
	githubProxy string
	injected    func(*catalog.Artifact) (artifact.MaterializedFile, error)
}

func (s *downloadStore) cacheHit() {
	if s.fetch != nil {
		s.fetch.Cached++
	}
}

func (s *downloadStore) downloaded(bytes int64) {
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
	hashers, writers, err := digestHashers(algorithms)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(io.MultiWriter(writers...), file); err != nil {
		return nil, err
	}
	return digestValues(hashers, algorithms, filename)
}

func digestHashers(algorithms map[string]string) (map[string]hash.Hash, []io.Writer, error) {
	hashers := map[string]hash.Hash{"sha256": sha256.New()}
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
			return nil, fmt.Errorf("%s checksum mismatch for %s", algorithm, filename)
		}
	}
	return values, nil
}

// materialize returns the verified store object and the digests computed while
// verifying it. The artifact is an input only; callers attach the returned
// hashes when the artifact becomes part of a lock. A store hit performs no
// network or cache access; a miss copies verified bytes from the network cache
// or the network into the store.
func (s *downloadStore) materialize(a *catalog.Artifact) (artifact.MaterializedFile, error) {
	if err := s.ctx.Err(); err != nil {
		return artifact.MaterializedFile{}, err
	}
	if a.Embedded != "" {
		return artifact.MaterializedFile{}, fmt.Errorf("embedded artifacts are extracted from their pinned parent")
	}
	if a.Filename == "" || filepath.Base(a.Filename) != a.Filename || strings.ContainsAny(a.Filename, "/\\") {
		return artifact.MaterializedFile{}, fmt.Errorf("invalid artifact filename %q", a.Filename)
	}
	if s.injected != nil {
		return s.injectedMaterialize(a)
	}
	store, err := artifactstore.Open()
	if err != nil {
		return artifact.MaterializedFile{}, err
	}
	defer store.Close()
	if expected := strings.ToLower(strings.TrimSpace(a.Hashes["sha256"])); expected != "" {
		digest, err := cas.ParseDigest("sha256:" + expected)
		if err != nil {
			return artifact.MaterializedFile{}, fmt.Errorf(
				"artifact %s has an invalid sha256 digest %q: %w",
				a.Filename, expected, err,
			)
		}
		destination := store.Path(digest)
		hashes, err := DigestFile(destination, a.Hashes)
		if err == nil {
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
			s.logger.Debug(
				"store object is unusable; acquiring again",
				"file", a.Filename,
				"error", err,
			)
		}
	}
	source, cleanup, err := s.downloadArtifact(a)
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
	s.logger.Debug(
		"artifact verified",
		"file", a.Filename,
		"sha256", hashes["sha256"],
	)
	return artifact.MaterializedFile{Path: store.Path(object.Digest), Hashes: hashes}, nil
}

// injectedMaterialize verifies bytes another operation supplied. Digests are
// recomputed from the returned file, so an injected materializer cannot lower
// the hash guarantee. Verified bytes enter the artifact store because a lock
// that mentions them must remain replayable. It does not touch the fetch
// summary, which describes the configured cache's transfers.
func (s *downloadStore) injectedMaterialize(a *catalog.Artifact) (artifact.MaterializedFile, error) {
	expected := maps.Clone(a.Hashes)
	input := *a
	input.Hashes = maps.Clone(a.Hashes)
	input.Modules = slices.Clone(a.Modules)
	for i := range input.Modules {
		input.Modules[i].Provides = slices.Clone(a.Modules[i].Provides)
		input.Modules[i].Dependencies = slices.Clone(a.Modules[i].Dependencies)
	}
	materialized, err := s.injected(&input)
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
	hashers, writers, err := digestHashers(expected)
	if err != nil {
		return cas.Object{}, nil, err
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
	object, err = writer.Commit(nil)
	if err != nil {
		return cas.Object{}, nil, err
	}
	return object, hashes, nil
}

// downloadArtifact obtains candidate bytes from the network cache or, in
// offline mode, the cache alone. The returned path is temporary; callers
// import it into the store before removing the directory.
func (s *downloadStore) downloadArtifact(a *catalog.Artifact) (string, func(), error) {
	directory, err := os.MkdirTemp("", "lucy-artifact-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(directory) }
	if s.offline {
		hit, cached, _, err := s.network.Get(a.URL)
		if err != nil {
			return "", cleanup, err
		}
		if !hit || cached == nil {
			return "", cleanup, fmt.Errorf("offline artifact unavailable: %s", a.URL)
		}
		defer cached.Close()
		destination := filepath.Join(directory, a.Filename)
		out, err := os.Create(destination)
		if err != nil {
			return "", cleanup, err
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
		s.trace.fetchStarted(a.Filename, size)
		return &fetchReader{
			reader: reader,
			name:   a.Filename,
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
	result, err := s.network.CachedDownload(s.ctx, a.URL, directory, options)
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
		s.trace.fetchProgress(a.Filename, read, total)
		s.trace.fetchDone(a.Filename, false)
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
	r.store.trace.fetchProgress(r.name, *r.read, r.total)
	return count, err
}
