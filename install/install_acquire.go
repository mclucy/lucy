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
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
)

type downloadStore struct {
	ctx     context.Context
	dir     string
	offline bool
}

// MaterializeServerProduct resolves and downloads the declared server product
// into the workspace download store.
func MaterializeServerProduct(ctx context.Context, root string, request types.ServerProductRequest) error {
	store := &downloadStore{ctx: ctx, dir: filepath.Join(root, ".lucy", "downloads")}
	_, err := routing.ResolveServerProduct(ctx, request, store.materialize)
	return err
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

func (s *downloadStore) materialize(a *types.Artifact) (string, error) {
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
func (s *downloadStore) materializeFile(a *types.Artifact, dir, destination string) error {
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
	} else {
		result, err := cache.CachedDownload(a.URL, dir, cache.DownloadOptions{Kind: cache.KindArtifact, Filename: a.Filename})
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
	}
	if _, err := DigestFile(staged, a.Hashes); err != nil {
		return err
	}
	return os.Rename(staged, destination)
}
