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
	"github.com/mclucy/lucy/types"
)

type artifactStore struct {
	ctx     context.Context
	dir     string
	offline bool
}

func digestFile(filename string, algorithms map[string]string) (map[string]string, error) {
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

func (s *artifactStore) acquire(a *types.Artifact) (string, error) {
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
	if _, err := os.Stat(destination); os.IsNotExist(err) {
		if s.offline {
			hit, cached, err := cache.Network().Get(a.URL)
			if err != nil {
				return "", err
			}
			if !hit || cached == nil {
				return "", fmt.Errorf("offline artifact unavailable: %s", a.URL)
			}
			defer cached.Close()
			out, err := os.Create(destination)
			if err != nil {
				return "", err
			}
			_, copyErr := io.Copy(out, cached)
			closeErr := out.Close()
			if copyErr != nil {
				return "", copyErr
			}
			if closeErr != nil {
				return "", closeErr
			}
		} else {
			result, err := cache.CachedDownload(a.URL, dir, cache.DownloadOptions{Kind: cache.KindArtifact, Filename: a.Filename})
			if err != nil {
				return "", err
			}
			actual := result.File.Name()
			if err := result.File.Close(); err != nil {
				return "", err
			}
			if actual != destination {
				if err := os.Rename(actual, destination); err != nil {
					return "", err
				}
			}
		}
	} else if err != nil {
		return "", err
	}
	hashes, err := digestFile(destination, a.Hashes)
	if err != nil {
		return "", err
	}
	a.Hashes = hashes
	return destination, nil
}
