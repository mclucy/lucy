package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
)

// acquireTestRemote serves one mutable download URL whose bytes a test can
// replace, and counts how many requests actually reached the server. Spiget
// download URLs are stable across releases while their bytes are not, which is
// the situation these tests reproduce.
type acquireTestRemote struct {
	url      string
	mu       sync.RWMutex
	payload  []byte
	requests atomic.Int64
	server   *httptest.Server
}

func newAcquireTestRemote(t *testing.T, payload []byte) *acquireTestRemote {
	t.Helper()
	remote := &acquireTestRemote{payload: append([]byte(nil), payload...)}
	remote.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		remote.requests.Add(1)
		remote.mu.RLock()
		payload := append([]byte(nil), remote.payload...)
		remote.mu.RUnlock()
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(payload)
	}))
	t.Cleanup(remote.server.Close)
	remote.url = remote.server.URL + "/download/123"
	return remote
}

func (r *acquireTestRemote) setPayload(payload []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.payload = append([]byte(nil), payload...)
}

// acquireTestCache roots a real cache client in a private directory so no
// test can see another test's entries.
func acquireTestCache(t *testing.T) *cache.Client {
	t.Helper()
	client := cache.New(
		t.TempDir(),
		cache.DefaultCacheConfig(),
		http.DefaultClient,
		nil,
	)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func acquireTestDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func acquireTestSpigetArtifact(remote *acquireTestRemote, releaseID, version string) *catalog.Artifact {
	return &catalog.Artifact{
		Provider:  catalog.ProviderSpiget,
		ProjectID: "123",
		ReleaseID: releaseID,
		FileID:    releaseID,
		Version:   versionpkg.Version(version),
		// An external Spiget download URL names the file itself, so two
		// releases can share a filename as well as a URL.
		Filename: "plugin.jar",
		URL:      remote.url,
	}
}

func acquireTestMaterialize(t *testing.T, store *downloadStore, a *catalog.Artifact) (map[string]string, []byte) {
	t.Helper()
	materialized, err := store.materialize(a)
	if err != nil {
		t.Fatalf("materialize %s release %s: %v", a.Filename, a.ReleaseID, err)
	}
	data, err := os.ReadFile(materialized.Path)
	if err != nil {
		t.Fatalf("read materialized %s: %v", a.Filename, err)
	}
	return materialized.Hashes, data
}

func acquireTestRequireBytes(t *testing.T, label string, got, want []byte) {
	t.Helper()
	if string(got) != string(want) {
		t.Fatalf("%s bytes = %q, want %q", label, got, want)
	}
}

func acquireTestRequireDigest(t *testing.T, label string, hashes map[string]string, want []byte) {
	t.Helper()
	if got, expected := hashes["sha256"], acquireTestDigest(want); got != expected {
		t.Fatalf("%s sha256 = %q, want %q", label, got, expected)
	}
}

// A Spiget download URL is mutable: the same URL serves different bytes for
// different releases, and Spiget supplies no upstream hash. The network cache
// must therefore key those bytes by release identity, not by URL alone.
// Otherwise a newer release silently consumes the older release's cached
// bytes, online and offline, while a pinned artifact keeps its store object.
// The cache identity must also retain each artifact's own URL so two distinct
// URLs never alias, and an artifact missing provider coordinates must fail
// closed instead of sharing a URL key with another release.
func TestMaterializeSpigetMutableURLDoesNotReuseAnotherReleasesBytes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "appdata"))

	t.Run("same mutable URL distinguishes releases", func(t *testing.T) {
		releaseOneBytes := []byte("bytes published by release 101\n")
		releaseTwoBytes := []byte("bytes published by release 102\n")

		remote := newAcquireTestRemote(t, releaseOneBytes)
		network := acquireTestCache(t)
		store := &downloadStore{ctx: context.Background(), network: network}

		releaseOne := acquireTestSpigetArtifact(remote, "101", "1.0.0")
		hashes, firstBytes := acquireTestMaterialize(t, store, releaseOne)
		acquireTestRequireBytes(t, "release 101", firstBytes, releaseOneBytes)
		acquireTestRequireDigest(t, "release 101", hashes, releaseOneBytes)
		if got := remote.requests.Load(); got != 1 {
			t.Fatalf("release 101 caused %d downloads, want 1", got)
		}

		// Before release 102 is ever downloaded, an offline lookup must miss
		// rather than borrow release 101 bytes cached under the shared URL.
		offline := &downloadStore{
			ctx:     context.Background(),
			offline: true,
			network: network,
		}
		releaseTwo := acquireTestSpigetArtifact(remote, "102", "2.0.0")
		if materialized, err := offline.materialize(releaseTwo); err == nil {
			t.Fatalf(
				"offline release 102 resolved without being cached (path %q)",
				materialized.Path,
			)
		}
		if got := remote.requests.Load(); got != 1 {
			t.Fatalf("offline miss contacted the server (%d downloads, want 1)", got)
		}

		// The publisher replaces the bytes behind the same stable URL.
		remote.setPayload(releaseTwoBytes)

		hashes, secondBytes := acquireTestMaterialize(t, store, releaseTwo)
		acquireTestRequireBytes(t, "release 102", secondBytes, releaseTwoBytes)
		acquireTestRequireDigest(t, "release 102", hashes, releaseTwoBytes)
		if got := remote.requests.Load(); got != 2 {
			t.Fatalf(
				"release 102 reused the cached release 101 bytes without a download (%d downloads, want 2)",
				got,
			)
		}

		// Repeating one release still resolves from its own cache entry. The
		// server now publishes release 102 bytes, so a network fetch would show
		// up both in the request count and in the returned bytes.
		hashes, repeatedBytes := acquireTestMaterialize(t, store, releaseOne)
		acquireTestRequireBytes(t, "release 101 replay", repeatedBytes, releaseOneBytes)
		acquireTestRequireDigest(t, "release 101 replay", hashes, releaseOneBytes)
		if got := remote.requests.Load(); got != 2 {
			t.Fatalf("release 101 replay contacted the server (%d downloads, want 2)", got)
		}

		// Offline runs may use the cache alone. Each already-materialized
		// release keeps its own cached bytes, and neither lookup may reach the
		// network.
		hashes, offlineOneBytes := acquireTestMaterialize(t, offline, releaseOne)
		acquireTestRequireBytes(t, "offline release 101", offlineOneBytes, releaseOneBytes)
		acquireTestRequireDigest(t, "offline release 101", hashes, releaseOneBytes)
		hashes, offlineTwoBytes := acquireTestMaterialize(t, offline, releaseTwo)
		acquireTestRequireBytes(t, "offline release 102", offlineTwoBytes, releaseTwoBytes)
		acquireTestRequireDigest(t, "offline release 102", hashes, releaseTwoBytes)
		if got := remote.requests.Load(); got != 2 {
			t.Fatalf("offline materialization contacted the server (%d downloads, want 2)", got)
		}

		// A pinned artifact from an immutable provider is unaffected: its store
		// object satisfies the expected digest even though the URL now serves
		// different bytes, so no download happens.
		pinned := &catalog.Artifact{
			Provider:  catalog.ProviderModrinth,
			ProjectID: "demo",
			ReleaseID: "r1",
			FileID:    "f1",
			Version:   versionpkg.Version("1.0.0"),
			Filename:  "plugin.jar",
			URL:       remote.url,
			Hashes:    map[string]string{"sha256": acquireTestDigest(releaseOneBytes)},
		}
		hashes, pinnedBytes := acquireTestMaterialize(t, store, pinned)
		acquireTestRequireBytes(t, "pinned non-Spiget artifact", pinnedBytes, releaseOneBytes)
		acquireTestRequireDigest(t, "pinned non-Spiget artifact", hashes, releaseOneBytes)
		if got := remote.requests.Load(); got != 2 {
			t.Fatalf("pinned artifact contacted the server (%d downloads, want 2)", got)
		}
	})

	t.Run("distinct URLs for the same coordinates do not alias", func(t *testing.T) {
		firstURLBytes := []byte("bytes served by the first URL\n")
		secondURLBytes := []byte("bytes served by the second URL\n")

		firstRemote := newAcquireTestRemote(t, firstURLBytes)
		secondRemote := newAcquireTestRemote(t, secondURLBytes)
		network := acquireTestCache(t)
		store := &downloadStore{ctx: context.Background(), network: network}

		// Both artifacts name the same release coordinates. Only their
		// download URLs differ, so a cache key that omits the URL would return
		// the first URL's bytes for the second URL.
		first := acquireTestSpigetArtifact(firstRemote, "101", "1.0.0")
		second := acquireTestSpigetArtifact(secondRemote, "101", "1.0.0")

		hashes, firstBytes := acquireTestMaterialize(t, store, first)
		acquireTestRequireBytes(t, "first URL", firstBytes, firstURLBytes)
		acquireTestRequireDigest(t, "first URL", hashes, firstURLBytes)
		if got := firstRemote.requests.Load(); got != 1 {
			t.Fatalf("first URL served %d downloads, want 1", got)
		}
		if got := secondRemote.requests.Load(); got != 0 {
			t.Fatalf("second URL served %d downloads before it was requested, want 0", got)
		}

		hashes, secondBytes := acquireTestMaterialize(t, store, second)
		acquireTestRequireBytes(t, "second URL", secondBytes, secondURLBytes)
		acquireTestRequireDigest(t, "second URL", hashes, secondURLBytes)
		if got := secondRemote.requests.Load(); got != 1 {
			t.Fatalf(
				"second URL reused the first URL's cached bytes without a download (%d downloads, want 1)",
				got,
			)
		}

		if _, repeatedBytes := acquireTestMaterialize(t, store, first); string(repeatedBytes) != string(firstURLBytes) {
			t.Fatalf("first URL replay bytes = %q, want %q", repeatedBytes, firstURLBytes)
		}
		if got := firstRemote.requests.Load(); got != 1 {
			t.Fatalf("first URL replay contacted the server (%d downloads, want 1)", got)
		}

		offline := &downloadStore{
			ctx:     context.Background(),
			offline: true,
			network: network,
		}
		if _, offlineBytes := acquireTestMaterialize(t, offline, first); string(offlineBytes) != string(firstURLBytes) {
			t.Fatalf("offline first URL bytes = %q, want %q", offlineBytes, firstURLBytes)
		}
		if _, offlineBytes := acquireTestMaterialize(t, offline, second); string(offlineBytes) != string(secondURLBytes) {
			t.Fatalf("offline second URL bytes = %q, want %q", offlineBytes, secondURLBytes)
		}
		if firstRemote.requests.Load() != 1 || secondRemote.requests.Load() != 1 {
			t.Fatalf(
				"offline materialization contacted a server (%d and %d downloads, want 1 and 1)",
				firstRemote.requests.Load(),
				secondRemote.requests.Load(),
			)
		}
	})

	t.Run("missing coordinates fail closed", func(t *testing.T) {
		remote := newAcquireTestRemote(t, []byte("bytes that must not be fetched\n"))
		network := acquireTestCache(t)
		store := &downloadStore{ctx: context.Background(), network: network}

		cases := []struct {
			name   string
			mutate func(*catalog.Artifact)
		}{
			{"missing project", func(a *catalog.Artifact) { a.ProjectID = "" }},
			{"missing release", func(a *catalog.Artifact) { a.ReleaseID = "" }},
			{"missing file", func(a *catalog.Artifact) { a.FileID = "" }},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				artifact := acquireTestSpigetArtifact(remote, "101", "1.0.0")
				tc.mutate(artifact)
				if materialized, err := store.materialize(artifact); err == nil {
					t.Fatalf(
						"materialize succeeded without complete coordinates (path %q)",
						materialized.Path,
					)
				}
				if got := remote.requests.Load(); got != 0 {
					t.Fatalf("incomplete coordinates caused %d downloads, want 0", got)
				}
			})
		}
	})
}
