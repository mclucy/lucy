package install

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/log"
)

type downloadStore struct {
	ctx     context.Context
	dir     string
	offline bool
	trace   *Trace
	fetch   *FetchSummary
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

func DigestFile(filename string, algorithms map[string]string) (map[string]string, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	hashes := map[string]hash.Hash{"sha256": sha256.New()}
	for algorithm := range algorithms {
		switch algorithm {
		case "sha1":
			hashes[algorithm] = sha1.New()
		case "sha512":
			hashes[algorithm] = sha512.New()
		case "md5":
			hashes[algorithm] = md5.New()
		case "sha256":
		default:
			return nil, fmt.Errorf("unsupported artifact digest %s", algorithm)
		}
	}
	writers := make([]io.Writer, 0, len(hashes))
	for _, h := range hashes {
		writers = append(writers, h)
	}
	if _, err := io.Copy(io.MultiWriter(writers...), f); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for algorithm, h := range hashes {
		out[algorithm] = hex.EncodeToString(h.Sum(nil))
		if expected := algorithms[algorithm]; expected != "" && out[algorithm] != strings.ToLower(expected) {
			return nil, fmt.Errorf("%s checksum mismatch for %s", algorithm, filename)
		}
	}
	return out, nil
}

func (s *downloadStore) materialize(a *catalog.Artifact) (string, error) {
	if err := s.ctx.Err(); err != nil {
		return "", err
	}
	if a.Embedded != "" {
		return "", fmt.Errorf("embedded artifacts are extracted from their pinned parent")
	}
	if a.Filename == "" || filepath.Base(a.Filename) != a.Filename || strings.ContainsAny(a.Filename, "/\\") {
		return "", fmt.Errorf("invalid artifact filename %q", a.Filename)
	}
	key := sha256.Sum256([]byte(a.URL))
	dir := filepath.Join(s.dir, hex.EncodeToString(key[:]))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	destination := filepath.Join(dir, a.Filename)
	source := "cache"
	hashes, err := DigestFile(destination, a.Hashes)
	if err != nil {
		if !os.IsNotExist(err) {
			// A destination that fails verification would be reused by every
			// later attempt, so discard it instead of leaving bad bytes.
			if err := os.Remove(destination); err != nil {
				return "", err
			}
		}
		if s.offline {
			source = "offline cache"
		} else {
			source = "download"
		}
		if err := s.materializeFile(a, dir, destination); err != nil {
			return "", err
		}
		if hashes, err = DigestFile(destination, a.Hashes); err != nil {
			os.Remove(destination)
			return "", err
		}
	} else {
		s.cacheHit()
	}
	a.Hashes = hashes
	log.Debug(
		"artifact verified",
		"file", a.Filename,
		"sha256", hashes["sha256"],
		"source", source,
	)
	return destination, nil
}

// materializeFile materializes the artifact at destination from the network or
// the offline cache. Bytes are staged under a partial name and renamed into
// place only after they verify, so an interrupted copy can never be mistaken
// for a completed download and a failed verification can never poison the
// store.
func (s *downloadStore) materializeFile(a *catalog.Artifact, dir, destination string) error {
	staged := destination + ".partial"
	defer os.Remove(staged)

	if s.offline {
		hit, cached, _, err := cache.Network().Get(a.URL)
		if err != nil {
			return err
		}
		if !hit || cached == nil {
			return fmt.Errorf("offline artifact unavailable: %s", a.URL)
		}
		defer cached.Close()
		out, err := os.Create(staged)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, cached)
		closeErr := out.Close()
		cachedErr := cached.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if cachedErr != nil {
			return cachedErr
		}
		s.cacheHit()
	} else {
		var read int64
		var total int64
		options := cache.DownloadOptions{
			Context:  s.ctx,
			Kind:     cache.KindArtifact,
			Filename: a.Filename,
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
		result, err := cache.CachedDownload(a.URL, dir, options)
		if err != nil {
			return err
		}
		actual := result.File.Name()
		if err := result.File.Close(); err != nil {
			return err
		}
		if actual != staged {
			if err := os.Rename(actual, staged); err != nil {
				return err
			}
		}
		if result.CacheHit {
			s.cacheHit()
		} else {
			s.downloaded(read)
			s.trace.fetchProgress(a.Filename, read, total)
			s.trace.fetchDone(a.Filename, false)
		}
	}
	if _, err := DigestFile(staged, a.Hashes); err != nil {
		return err
	}
	return os.Rename(staged, destination)
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
