//go:build integration

package cache

import (
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mclucy/lucy/cas"
)

func TestAliasKeysShareBlobUntilTheLastReferenceGoes(t *testing.T) {
	fixture := newCacheFixture(t)
	h := installNetworkCache(t, DefaultCacheConfig())
	first := fixture.duplicateURL("a.jar")
	second := fixture.duplicateURL("b.jar")
	payload := []byte(duplicatePayload)

	firstResult, err := CachedDownload(
		first,
		t.TempDir(),
		DownloadOptions{Kind: KindArtifact},
	)
	if err != nil {
		t.Fatalf("download the first alias: %v", err)
	}
	_ = firstResult.File.Close()
	secondResult, err := CachedDownload(
		second,
		t.TempDir(),
		DownloadOptions{Kind: KindArtifact},
	)
	if err != nil {
		t.Fatalf("download the second alias: %v", err)
	}
	_ = secondResult.File.Close()

	entries := h.All()
	if len(entries) != 2 {
		t.Fatalf("index holds %d entries, want 2", len(entries))
	}
	if entries[0].ContentHash != entries[1].ContentHash {
		t.Fatalf(
			"alias keys hold different blobs: %s and %s",
			entries[0].ContentHash,
			entries[1].ContentHash,
		)
	}
	if entries[0].Filename != "indexed-a.jar" ||
		entries[1].Filename != "indexed-b.jar" {
		t.Fatalf(
			"index filenames %q and %q, want two distinct indexed names",
			entries[0].Filename,
			entries[1].Filename,
		)
	}
	if objects := cacheObjects(t, h); len(objects) != 1 {
		t.Fatalf("cache store holds %d objects, want 1", len(objects))
	}

	// Dropping one reference must leave the blob for the surviving alias.
	if err := Network().Remove(first); err != nil {
		t.Fatalf("remove the first alias: %v", err)
	}
	remaining := h.All()
	if len(remaining) != 1 || remaining[0].Key != second {
		t.Fatalf("index holds %+v, want only the surviving alias", remaining)
	}
	if objects := cacheObjects(t, h); len(objects) != 1 {
		t.Fatalf(
			"blob was reclaimed while alias %s still referenced it",
			second,
		)
	}

	surviving := t.TempDir()
	hit, err := CachedDownload(
		second,
		surviving,
		DownloadOptions{Kind: KindArtifact},
	)
	if err != nil {
		t.Fatalf("download after the first reference was removed: %v", err)
	}
	_ = hit.File.Close()
	if !hit.CacheHit {
		t.Fatal("surviving alias missed the shared blob")
	}
	if fixture.downloads() != 2 {
		t.Fatalf(
			"surviving alias contacted the server (%d downloads total)",
			fixture.downloads(),
		)
	}
	requireFileBytes(t, filepath.Join(surviving, "indexed-b.jar"), payload)

	// The last reference releases the blob.
	if err := Network().Remove(second); err != nil {
		t.Fatalf("remove the surviving alias: %v", err)
	}
	if objects := cacheObjects(t, h); len(objects) != 0 {
		t.Fatalf("cache store holds %d objects, want 0", len(objects))
	}
	if found, _, err := h.GetBytes(second); err != nil || found {
		t.Fatalf("removed alias still reports a hit (hit=%v, err=%v)", found, err)
	}
}

func TestCacheEvictionCountsSharedObjectsOnce(t *testing.T) {
	cfg := DefaultCacheConfig()
	cfg.DownloadMaxSize = 250
	h := installNetworkCache(t, cfg)
	shared := bytes.Repeat([]byte("shared"), 20)
	unique := bytes.Repeat([]byte("unique"), 20)

	for i, key := range []string{"key-shared-a", "key-shared-b", "key-shared-c"} {
		if err := h.Add(shared, "shared.jar", key, time.Duration(i+1)*time.Hour); err != nil {
			t.Fatalf("add alias %s: %v", key, err)
		}
	}
	if err := h.Add(unique, "unique.jar", "key-unique", 5*time.Hour); err != nil {
		t.Fatalf("add the unique entry: %v", err)
	}
	for _, key := range []string{"key-shared-a", "key-shared-b", "key-shared-c"} {
		hit, data, err := h.GetBytes(key)
		if err != nil || !hit || !bytes.Equal(data, shared) {
			t.Fatalf("shared object within quota lost alias %s (hit=%v, err=%v, data=%q)", key, hit, err, data)
		}
	}

	newest := bytes.Repeat([]byte("newest"), 20)
	if err := h.Add(newest, "newest.jar", "key-newest", 6*time.Hour); err != nil {
		t.Fatalf("add the entry exceeding physical quota: %v", err)
	}
	for _, key := range []string{"key-shared-a", "key-shared-b", "key-shared-c"} {
		if h.Exist(key) {
			t.Fatalf("alias %s survived after its object was selected for eviction", key)
		}
	}
	for _, test := range []struct {
		key  string
		data []byte
	}{{"key-unique", unique}, {"key-newest", newest}} {
		hit, data, err := h.GetBytes(test.key)
		if err != nil || !hit || !bytes.Equal(data, test.data) {
			t.Fatalf("retained object %s is unreadable (hit=%v, err=%v, data=%q)", test.key, hit, err, data)
		}
	}
	objects := cacheObjects(t, h)
	if len(objects) != 2 {
		t.Fatalf("cache store holds %d objects, want the two retained objects", len(objects))
	}
}

func TestCacheClearReportsUniqueObjectsAndKeepsProjectStore(t *testing.T) {
	fixture := newCacheFixture(t)
	h := installNetworkCache(t, DefaultCacheConfig())
	for _, url := range []string{
		fixture.duplicateURL("shared-a.jar"),
		fixture.duplicateURL("shared-b.jar"),
		fixture.artifactURL("unique.jar"),
		fixture.artifactURL("empty.jar"),
	} {
		result, err := CachedDownload(url, t.TempDir(), DownloadOptions{Kind: KindArtifact})
		if err != nil {
			t.Fatalf("download %s: %v", url, err)
		}
		_ = result.File.Close()
	}
	if entries := h.All(); len(entries) != 4 {
		t.Fatalf("index holds %d entries, want 4", len(entries))
	}
	objects := cacheObjects(t, h)
	if len(objects) != 3 {
		t.Fatalf("cache store holds %d objects, want 3 including the empty blob", len(objects))
	}
	var uniqueBytes int64
	for _, object := range objects {
		uniqueBytes += object.Size
	}

	// A project-local store lives outside the cache root and is owned by the
	// workspace, so clearing the download cache must not reach it.
	projectRoot := t.TempDir()
	project, err := cas.New(filepath.Join(projectRoot, ".lucy"))
	if err != nil {
		t.Fatalf("open the project store: %v", err)
	}
	projectBytes := []byte("project tracked file bytes")
	object, err := project.Put(bytes.NewReader(projectBytes), nil)
	if err != nil {
		t.Fatalf("store project bytes: %v", err)
	}
	if err := project.Close(); err != nil {
		t.Fatalf("close the project store: %v", err)
	}
	if rel, err := filepath.Rel(h.dir, projectRoot); err == nil &&
		!strings.HasPrefix(rel, "..") {
		t.Fatalf("project store %s lives inside the cache root %s", projectRoot, h.dir)
	}

	report, err := h.ClearAll()
	if err != nil {
		t.Fatalf("clear the cache: %v", err)
	}
	if report.FileCount != len(objects) {
		t.Fatalf("clear removed %d files, want %d unique objects", report.FileCount, len(objects))
	}
	if report.TotalFreedSize != uniqueBytes {
		t.Fatalf(
			"clear freed %d bytes, want %d",
			report.TotalFreedSize,
			uniqueBytes,
		)
	}
	if entries := h.All(); len(entries) != 0 {
		t.Fatalf("index holds %d entries after clearing, want 0", len(entries))
	}
	if remaining := cacheObjects(t, h); len(remaining) != 0 {
		t.Fatalf("cache store holds %d objects after clearing, want 0", len(remaining))
	}

	reopened, err := cas.New(filepath.Join(projectRoot, ".lucy"))
	if err != nil {
		t.Fatalf("reopen the project store: %v", err)
	}
	defer reopened.Close()
	reader, err := reopened.Open(object.Digest)
	if err != nil {
		t.Fatalf("read the project object: %v", err)
	}
	data, err := io.ReadAll(reader)
	if closeErr := reader.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatalf("read the project object: %v", err)
	}
	if !bytes.Equal(data, projectBytes) {
		t.Fatalf("project object holds %q, want %q", data, projectBytes)
	}
}
