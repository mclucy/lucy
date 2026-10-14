//go:build integration

package cache

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mclucy/lucy/cas"
)

// seedLegacyCache writes the disposable index and blob layout the content-store
// cutover replaced: a version 2 manifest plus one directory per content hash.
// The bytes it leaves behind are old revisions of the same URL, so a cold start
// that reuses them would materialize the wrong content.
func seedLegacyCache(
	t *testing.T,
	dir string,
	cacheKey string,
	payload []byte,
) string {
	t.Helper()
	sum := sha256.Sum256(payload)
	digest := hex.EncodeToString(sum[:])
	blob := filepath.Join(dir, digest, "legacy.jar")
	if err := os.MkdirAll(filepath.Dir(blob), 0o700); err != nil {
		t.Fatalf("create the legacy blob directory: %v", err)
	}
	if err := os.WriteFile(blob, payload, 0o600); err != nil {
		t.Fatalf("write the legacy blob %s: %v", blob, err)
	}
	manifest := indexManifest{
		Version: 2,
		Entries: map[key]*CacheEntry{
			key(cacheKey): {
				Kind:        KindArtifact,
				Filename:    "legacy.jar",
				Size:        int64(len(payload)),
				ContentHash: "sha256:" + digest,
				Integrity:   Integrity{State: IntegrityUnverified},
				Expiration:  time.Now().Add(time.Hour),
				Key:         cacheKey,
				CreatedAt:   time.Now().Add(-time.Hour),
			},
		},
	}
	encoded, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("encode the legacy index: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, manifestFilename),
		encoded,
		0o600,
	); err != nil {
		t.Fatalf("write the legacy index: %v", err)
	}
	return digest
}

func TestCacheColdCutoverFromLegacyIndexRefills(t *testing.T) {
	base := t.TempDir()
	cacheHome := filepath.Join(base, "cache-home")
	fixture := newCacheFixture(t)
	legacyURL := fixture.artifactURL("legacy.jar")
	legacyBytes := []byte("legacy cached artifact bytes")

	first := installNetworkCacheAt(t, cacheHome, DefaultCacheConfig())
	legacyDigest := seedLegacyCache(t, first.dir, legacyURL, legacyBytes)

	projectRoot := filepath.Join(base, "project")
	project, err := cas.New(filepath.Join(projectRoot, ".lucy"))
	if err != nil {
		t.Fatalf("open the project store: %v", err)
	}
	projectBytes := []byte("project tracked file bytes")
	projectObject, err := project.Put(bytes.NewReader(projectBytes), nil)
	if err != nil {
		t.Fatalf("store project bytes: %v", err)
	}
	if err := project.Close(); err != nil {
		t.Fatalf("close the project store: %v", err)
	}

	// A new handler on the same root observes the previous disposable format.
	cold := installNetworkCacheAt(t, cacheHome, DefaultCacheConfig())
	if entries := cold.All(); len(entries) != 0 {
		t.Fatalf("the cutover kept %d legacy entries, want 0", len(entries))
	}
	if cold.Exist(legacyURL) {
		t.Fatal("the cutover kept the legacy entry")
	}
	if _, err := os.Stat(filepath.Join(cold.dir, legacyDigest)); !os.IsNotExist(err) {
		t.Fatalf("legacy blob at %s: %v", filepath.Join(cold.dir, legacyDigest), err)
	}

	payload := []byte(fixturePayload("legacy.jar"))
	sum := sha256.Sum256(payload)
	charged := t.TempDir()
	result, err := CachedDownload(
		legacyURL,
		charged,
		DownloadOptions{
			Kind:          KindArtifact,
			ExpectedHash:  digestHex(t, HashSHA1, payload),
			HashAlgorithm: HashSHA1,
		},
	)
	if err != nil {
		t.Fatalf("refill after the cutover: %v", err)
	}
	defer result.File.Close()
	if result.CacheHit {
		t.Fatal("the cutover served legacy bytes as a cache hit")
	}
	if !result.Verified {
		t.Fatal("refilled download was not verified")
	}
	if fixture.downloads() != 1 {
		t.Fatalf("server served %d downloads, want 1", fixture.downloads())
	}
	requireDirEntries(t, charged, "indexed-legacy.jar")
	requireFileBytes(t, filepath.Join(charged, "indexed-legacy.jar"), payload)

	// The refilled entry is now content-addressed storage.
	entries := cold.All()
	if len(entries) != 1 {
		t.Fatalf("index holds %d entries, want 1", len(entries))
	}
	if entries[0].ContentHash != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatalf("index addresses %s, want the refilled content", entries[0].ContentHash)
	}
	objects := cacheObjects(t, cold)
	if len(objects) != 1 || objects[0].Digest.String() != entries[0].ContentHash {
		t.Fatalf("cache store holds %+v, want one object at %s", objects, entries[0].ContentHash)
	}
	digest, err := cas.ParseDigest(entries[0].ContentHash)
	if err != nil {
		t.Fatalf("parse the stored digest: %v", err)
	}
	reader, err := cold.store.Open(digest)
	if err != nil {
		t.Fatalf("read the stored object: %v", err)
	}
	stored, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatalf("read the stored object: %v", err)
	}
	if !bytes.Equal(stored, payload) {
		t.Fatalf("stored object holds %q, want %q", stored, payload)
	}

	rehit := t.TempDir()
	again, err := CachedDownload(legacyURL, rehit, DownloadOptions{Kind: KindArtifact})
	if err != nil {
		t.Fatalf("download after the refill: %v", err)
	}
	defer again.File.Close()
	if !again.CacheHit {
		t.Fatal("refilled entry did not serve as a cache hit")
	}
	if fixture.downloads() != 1 {
		t.Fatalf(
			"cache hit contacted the server (%d downloads total)",
			fixture.downloads(),
		)
	}
	requireFileBytes(t, filepath.Join(rehit, "indexed-legacy.jar"), payload)

	reopened, err := cas.New(filepath.Join(projectRoot, ".lucy"))
	if err != nil {
		t.Fatalf("reopen the project store: %v", err)
	}
	defer reopened.Close()
	objectReader, err := reopened.Open(projectObject.Digest)
	if err != nil {
		t.Fatalf("read the project object: %v", err)
	}
	kept, readErr := io.ReadAll(objectReader)
	closeErr = objectReader.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatalf("read the project object: %v", err)
	}
	if !bytes.Equal(kept, projectBytes) {
		t.Fatalf("project object holds %q, want %q", kept, projectBytes)
	}
}
