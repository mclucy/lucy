package cache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestClientCloseUnusedDoesNotCreateRoot(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "cache")
	client := New(root, DefaultCacheConfig(), nil, nil)
	if err := client.Close(); err != nil {
		t.Fatalf("close unused client: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close unused client twice: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("closing an unused client created %s: %v", root, err)
	}
	if client.Exist("anything") {
		t.Fatal("closed unused client reported a hit")
	}
	hit, reader, _, err := client.Get("anything")
	if err != nil || hit || reader != nil {
		t.Fatalf("closed unused Get = %v, %v, %v", hit, reader, err)
	}
	if err := client.Flush(); err != nil {
		t.Fatalf("flush closed unused client: %v", err)
	}
}

func TestClientGetKeepsReaderAcrossIndexChanges(t *testing.T) {
	t.Parallel()
	fixture := newCacheFixture(t)
	client := installNetworkCache(t, DefaultCacheConfig())
	first := fixture.artifactURL("first.jar")
	second := fixture.artifactURL("second.jar")
	content := []byte(fixturePayload("first.jar"))
	firstResult, err := client.CachedDownload(
		context.Background(),
		first,
		t.TempDir(),
		DownloadOptions{Kind: KindArtifact, Filename: "first.jar"},
	)
	if err != nil {
		t.Fatalf("cache first entry: %v", err)
	}
	_ = firstResult.File.Close()
	hit, reader, filename, err := client.Get(first)
	if err != nil {
		t.Fatalf("get first entry: %v", err)
	}
	if !hit || reader == nil {
		t.Fatal("get missed an existing entry")
	}
	if filename != "first.jar" {
		t.Fatalf("get filename = %q, want first.jar", filename)
	}
	secondResult, err := client.CachedDownload(
		context.Background(),
		second,
		t.TempDir(),
		DownloadOptions{Kind: KindArtifact, Filename: "second.jar"},
	)
	if err != nil {
		t.Fatalf("cache second entry: %v", err)
	}
	_ = secondResult.File.Close()
	data, readErr := io.ReadAll(reader)
	closeErr := reader.Close()
	if err := errors.Join(readErr, closeErr); err != nil {
		t.Fatalf("read retained reader: %v", err)
	}
	if !bytes.Equal(data, content) {
		t.Fatalf("reader returned %q, want %q", data, content)
	}

	hit, reader, filename, err = client.Get("missing")
	if err != nil || hit || reader != nil || filename != "" {
		t.Fatalf("missing Get = %v, %q, %v, %v", hit, filename, reader, err)
	}
}

func TestClientGetRejectsExpiredEntry(t *testing.T) {
	t.Parallel()
	fixture := newCacheFixture(t)
	client := installNetworkCache(t, DefaultCacheConfig())
	entryKey := fixture.artifactURL("expired.jar")
	data, err := client.CachedGetBytes(
		context.Background(),
		entryKey,
		BytesRequestOptions{Kind: KindArtifact},
	)
	if err != nil {
		t.Fatalf("cache entry: %v", err)
	}
	if want := []byte(fixturePayload("expired.jar")); !bytes.Equal(data, want) {
		t.Fatalf("cached bytes = %q, want %q", data, want)
	}
	hit, reader, _, err := client.Get(entryKey)
	if err != nil || !hit || reader == nil {
		t.Fatalf("live Get = %v, %v, %v", hit, reader, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close live reader: %v", err)
	}

	handler := client.cache()
	handler.mu.Lock()
	entry, ok := handler.index.get(canonicalizeKey(entryKey))
	if ok {
		entry.Expiration = time.Now().Add(-time.Second)
	}
	handler.mu.Unlock()
	if !ok {
		t.Fatal("cached entry is missing from the index")
	}

	hit, reader, _, err = client.Get(entryKey)
	if err != nil || hit || reader != nil {
		t.Fatalf("expired Get = %v, %v, %v", hit, reader, err)
	}
}

func TestClientFlushPersistsIndex(t *testing.T) {
	t.Parallel()
	fixture := newCacheFixture(t)
	root := filepath.Join(t.TempDir(), "cache")
	client := installNetworkCacheAt(t, root, DefaultCacheConfig())
	result, err := client.CachedDownload(
		context.Background(),
		fixture.artifactURL("flushed.jar"),
		t.TempDir(),
		DownloadOptions{Kind: KindArtifact, Filename: "flushed.jar"},
	)
	if err != nil {
		t.Fatalf("cache entry: %v", err)
	}
	_ = result.File.Close()
	entries := client.All()
	if len(entries) != 1 {
		t.Fatalf("index holds %d entries, want 1", len(entries))
	}
	entry := *entries[0]
	original := key(entry.Key)
	entry.Key = "key-flushed"
	client.cache().index.delete(original)
	client.cache().index.put("key-flushed", &entry)
	if err := client.Flush(); err != nil {
		t.Fatalf("flush index: %v", err)
	}

	reopened := New(root, DefaultCacheConfig(), nil, nil)
	flushed := reopened.All()
	if len(flushed) != 1 || flushed[0].Key != "key-flushed" {
		t.Fatalf("reopened index = %+v, want the flushed entry", flushed)
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened client: %v", err)
	}
}

func TestClientCloseDisablesOpenedStore(t *testing.T) {
	t.Parallel()
	fixture := newCacheFixture(t)
	client := installNetworkCache(t, DefaultCacheConfig())
	entryKey := fixture.artifactURL("file.jar")
	if _, err := client.CachedGetBytes(
		context.Background(),
		entryKey,
		BytesRequestOptions{Kind: KindArtifact},
	); err != nil {
		t.Fatalf("cache entry: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
	if client.Exist(entryKey) {
		t.Fatal("closed client reported a hit")
	}
	hit, reader, _, err := client.Get(entryKey)
	if err != nil || hit || reader != nil {
		t.Fatalf("closed Get = %v, %v, %v", hit, reader, err)
	}
	if entries := client.All(); len(entries) != 0 {
		t.Fatalf("closed client lists %d entries", len(entries))
	}
	if err := client.Flush(); err != nil {
		t.Fatalf("flush closed client: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close client twice: %v", err)
	}
}

func TestClientInitializationResetsCorruptIndex(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "cache")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatalf("create cache root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, manifestFilename), []byte("{"), 0o600); err != nil {
		t.Fatalf("write corrupt index: %v", err)
	}
	client := New(root, DefaultCacheConfig(), nil, nil)
	if entries := client.All(); len(entries) != 0 {
		t.Fatalf("corrupt index produced %d entries", len(entries))
	}
	reopened := New(root, DefaultCacheConfig(), nil, nil)
	if entries := reopened.All(); len(entries) != 0 {
		t.Fatalf("reset index produced %d entries", len(entries))
	}
	if err := reopened.Close(); err != nil {
		t.Fatalf("close reopened client: %v", err)
	}
}

func TestClientRejectsInvalidConfigWithoutCreatingRoot(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "cache")
	config := DefaultCacheConfig()
	config.DownloadMaxSize = 0
	client := New(root, config, nil, nil)
	if entries := client.All(); len(entries) != 0 {
		t.Fatalf("invalid config produced %d entries", len(entries))
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("invalid config created %s: %v", root, err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close invalid client: %v", err)
	}
}
