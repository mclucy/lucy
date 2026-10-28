//go:build integration

package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

// TestCacheFallbackProcess is the helper half of
// TestMetadataFallbackWithoutTempDir. It blocks the cache
// root with a regular file so the real handler starts disabled, and points
// TMPDIR at a path whose parent does not exist.
func TestCacheFallbackProcess(t *testing.T) {
	scenario, isChild := cacheChildScenario()
	if !isChild || scenario != "fallback" {
		t.Skip("helper process started by the disabled cache fallback test")
	}
	if directory, err := os.MkdirTemp("", "lucy-cache-probe-"); err == nil {
		_ = os.RemoveAll(directory)
		t.Fatal("TMPDIR accepted a temporary directory, so the fallback is untested")
	}

	blocked := DefaultRoot()
	if err := os.MkdirAll(filepath.Dir(blocked), 0o700); err != nil {
		t.Fatalf("prepare the unusable cache root: %v", err)
	}
	if err := os.WriteFile(blocked, []byte("not a cache directory"), 0o600); err != nil {
		t.Fatalf("block the cache root %s: %v", blocked, err)
	}

	client := New(blocked, DefaultCacheConfig(), http.DefaultClient, nil)
	h := client.cache()
	if h.on {
		t.Fatalf("cache handler started in the blocked root %s", h.dir)
	}

	server := os.Getenv(cacheChildServer)
	if server == "" {
		t.Fatalf("%s is not set", cacheChildServer)
	}
	data, err := client.CachedGetBytes(context.Background(), server, BytesRequestOptions{Kind: KindMetadata})
	if err != nil {
		t.Fatalf("metadata fetch without a usable cache: %v", err)
	}
	sum := sha256.Sum256(data)
	reportCacheChild(
		t,
		cacheChildReport{
			Scenario:   scenario,
			CacheOn:    h.on,
			CacheDir:   h.dir,
			TempUsable: false,
			CacheHit:   false,
			DataSHA256: hex.EncodeToString(sum[:]),
			DataLength: len(data),
		},
	)
}

func TestMetadataFallbackWithoutTempDir(t *testing.T) {
	root := t.TempDir()
	fixture := newCacheFixture(t)
	home := filepath.Join(root, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatalf("prepare the helper home: %v", err)
	}
	report := runCacheChild(
		t,
		"TestCacheFallbackProcess",
		map[string]string{
			"HOME":           home,
			"XDG_CACHE_HOME": filepath.Join(home, ".cache"),
			"LOCALAPPDATA":   filepath.Join(home, ".cache"),
			"TMP":            filepath.Join(root, "missing-parent", "deeper"),
			"TEMP":           filepath.Join(root, "missing-parent", "deeper"),
			"TMPDIR":         filepath.Join(root, "missing-parent", "deeper"),
			cacheChildServer: fixture.artifactURL("metadata.json"),
			cacheChildJob:    "fallback",
		},
	)

	if report.TempUsable {
		t.Fatal("helper reported a usable TMPDIR")
	}
	if report.CacheOn {
		t.Fatalf(
			"cache handler started from the blocked root %s",
			report.CacheDir,
		)
	}
	if report.CacheHit {
		t.Fatal("disabled cache reported a hit")
	}
	if fixture.downloads() != 1 {
		t.Fatalf("server served %d requests, want 1", fixture.downloads())
	}
	want := []byte(fixturePayload("metadata.json"))
	sum := sha256.Sum256(want)
	if report.DataSHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf(
			"helper fetched %q bytes, want %q",
			report.DataSHA256,
			hex.EncodeToString(sum[:]),
		)
	}
	if report.DataLength != len(want) {
		t.Fatalf("helper fetched %d bytes, want %d", report.DataLength, len(want))
	}
}
