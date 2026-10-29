package cache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestByteStageDetachContinuesHash(t *testing.T) {
	t.Parallel()
	client := installNetworkCache(t, DefaultCacheConfig())
	stage := client.newByteStage()
	if stage.writer == nil {
		t.Fatal("enabled cache did not stage bytes")
	}
	prefix := []byte("prefix held by the cache stage\n")
	suffix := []byte("suffix retained after the stage failed\n")
	if _, err := stage.Write(prefix); err != nil {
		t.Fatalf("write prefix: %v", err)
	}
	if err := stage.detach(); err != nil {
		t.Fatalf("detach stage: %v", err)
	}
	if stage.writer != nil {
		t.Fatal("detached stage kept its writer")
	}
	if _, err := stage.Write(suffix); err != nil {
		t.Fatalf("write suffix after detach: %v", err)
	}
	object, err := stage.seal()
	if err != nil {
		t.Fatalf("seal detached stage: %v", err)
	}
	want := testDigest(string(append(append([]byte{}, prefix...), suffix...)))
	if object.Digest.String() != want {
		t.Fatalf("detached stage digest = %s, want %s", object.Digest, want)
	}
	if err := stage.Close(); err != nil {
		t.Fatalf("close detached stage: %v", err)
	}
}

func TestByteStageDisabledClientKeepsBytesInMemory(t *testing.T) {
	t.Parallel()
	client := New(
		filepath.Join(t.TempDir(), "cache"),
		Config{Enabled: false},
		nil,
		nil,
	)
	stage := client.newByteStage()
	if stage.writer != nil {
		t.Fatal("disabled cache opened a staging writer")
	}
	content := []byte("bytes that never reach the cache\n")
	if _, err := stage.Write(content); err != nil {
		t.Fatalf("write disabled stage: %v", err)
	}
	object, err := stage.seal()
	if err != nil {
		t.Fatalf("seal disabled stage: %v", err)
	}
	if object.Digest.String() != testDigest(string(content)) {
		t.Fatalf("disabled stage digest = %s", object.Digest)
	}
	if err := stage.Close(); err != nil {
		t.Fatalf("close disabled stage: %v", err)
	}
}

func TestDownloadStageFallsBackWhenCacheIsDisabled(t *testing.T) {
	t.Parallel()
	client := New(
		filepath.Join(t.TempDir(), "cache"),
		Config{Enabled: false},
		nil,
		nil,
	)
	stage, err := client.newDownloadStage()
	if err != nil {
		t.Fatalf("create fallback download stage: %v", err)
	}
	if stage.cacheable {
		t.Fatal("fallback download stage remained cacheable")
	}
	if stage.temporary == nil || stage.directory == "" {
		t.Fatal("fallback download stage has no temporary store")
	}
	content := []byte("fallback download contents\n")
	if _, err := stage.Write(content); err != nil {
		t.Fatalf("write fallback stage: %v", err)
	}
	object, err := stage.seal()
	if err != nil {
		t.Fatalf("seal fallback stage: %v", err)
	}
	if object.Digest.String() != testDigest(string(content)) {
		t.Fatalf("fallback stage digest = %s", object.Digest)
	}
	if object.Size != int64(len(content)) {
		t.Fatalf("fallback stage size = %d, want %d", object.Size, len(content))
	}
	directory := stage.directory
	if err := stage.Close(); err != nil {
		t.Fatalf("close fallback stage: %v", err)
	}
	if _, err := os.Stat(directory); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fallback stage left %s behind: %v", directory, err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("close client: %v", err)
	}
}

func TestDownloadStageFallbackPreservesWrittenPrefix(t *testing.T) {
	t.Parallel()
	root := filepath.Join(t.TempDir(), "cache")
	client := installNetworkCacheAt(t, root, DefaultCacheConfig())
	stage, err := client.newDownloadStage()
	if err != nil {
		t.Fatalf("create download stage: %v", err)
	}
	if !stage.cacheable {
		t.Fatal("enabled cache did not create a cacheable stage")
	}
	prefix := []byte("prefix written before the cache failed\n")
	suffix := []byte("suffix written after the cache failed\n")
	if _, err := stage.Write(prefix); err != nil {
		t.Fatalf("write prefix: %v", err)
	}
	if err := stage.fallback(); err != nil {
		t.Fatalf("fall back: %v", err)
	}
	if stage.cacheable {
		t.Fatal("fallback stage remained cacheable")
	}
	if _, err := stage.Write(suffix); err != nil {
		t.Fatalf("write suffix: %v", err)
	}
	object, err := stage.seal()
	if err != nil {
		t.Fatalf("seal fallback stage: %v", err)
	}
	want := testDigest(string(append(append([]byte{}, prefix...), suffix...)))
	if object.Digest.String() != want {
		t.Fatalf("fallback stage digest = %s, want %s", object.Digest, want)
	}
	if err := stage.Close(); err != nil {
		t.Fatalf("close fallback stage: %v", err)
	}
}
