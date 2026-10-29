package cache

import (
	"bytes"
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
	client := installNetworkCache(t, DefaultCacheConfig())
	content := []byte("reader lifetime contents\n")
	if err := client.cache().add(content, "first.jar", "key-first", time.Hour); err != nil {
		t.Fatalf("add first entry: %v", err)
	}
	hit, reader, filename, err := client.Get("key-first")
	if err != nil {
		t.Fatalf("get first entry: %v", err)
	}
	if !hit || reader == nil {
		t.Fatal("get missed an existing entry")
	}
	if filename != "first.jar" {
		t.Fatalf("get filename = %q, want first.jar", filename)
	}
	if err := client.cache().add([]byte("other contents"), "second.jar", "key-second", time.Hour); err != nil {
		t.Fatalf("add second entry: %v", err)
	}
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
	client := installNetworkCache(t, DefaultCacheConfig())
	content := []byte("expired entry")
	if err := client.cache().addEntry(
		content,
		"expired.jar",
		"key-expired",
		KindArtifact,
		Integrity{State: IntegrityUnverified},
		-time.Hour,
	); err != nil {
		t.Fatalf("add expired entry: %v", err)
	}
	hit, reader, _, err := client.Get("key-expired")
	if err != nil || hit || reader != nil {
		t.Fatalf("expired Get = %v, %v, %v", hit, reader, err)
	}
}

func TestClientFlushPersistsIndex(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "cache")
	client := installNetworkCacheAt(t, root, DefaultCacheConfig())
	if err := client.cache().add([]byte("flushed contents"), "flushed.jar", "key-original", time.Hour); err != nil {
		t.Fatalf("add entry: %v", err)
	}
	entries := client.All()
	if len(entries) != 1 {
		t.Fatalf("index holds %d entries, want 1", len(entries))
	}
	entry := *entries[0]
	entry.Key = "key-flushed"
	client.cache().index.delete("key-original")
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
	client := installNetworkCache(t, DefaultCacheConfig())
	if err := client.cache().add([]byte("contents"), "file.jar", "key", time.Hour); err != nil {
		t.Fatalf("add entry: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
	if client.Exist("key") {
		t.Fatal("closed client reported a hit")
	}
	hit, reader, _, err := client.Get("key")
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
