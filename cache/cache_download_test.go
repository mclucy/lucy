package cache

import (
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mclucy/lucy/cas"
)

const (
	artifactRoute  = "/artifact/"
	duplicateRoute = "/duplicate/"

	// duplicatePayload is served from several routes so distinct cache keys
	// share one content-addressed blob.
	duplicatePayload = "bytes shared by several cache keys"
)

// cacheFixture serves deterministic artifact bytes over real HTTP and counts
// the requests each route receives, so a test can tell a refetch from a hit.
type cacheFixture struct {
	server   *httptest.Server
	requests atomic.Int64
}

func newCacheFixture(t *testing.T) *cacheFixture {
	t.Helper()
	fixture := &cacheFixture{}
	mux := http.NewServeMux()
	mux.HandleFunc(
		artifactRoute,
		func(w http.ResponseWriter, r *http.Request) {
			fixture.requests.Add(1)
			name := strings.TrimPrefix(r.URL.Path, artifactRoute)
			writeFixtureArtifact(w, name, fixturePayload(name))
		},
	)
	mux.HandleFunc(
		duplicateRoute,
		func(w http.ResponseWriter, r *http.Request) {
			fixture.requests.Add(1)
			name := strings.TrimPrefix(r.URL.Path, duplicateRoute)
			writeFixtureArtifact(w, name, duplicatePayload)
		},
	)
	fixture.server = httptest.NewServer(mux)
	t.Cleanup(fixture.server.Close)
	return fixture
}

func (f *cacheFixture) artifactURL(name string) string {
	return f.server.URL + artifactRoute + name
}

func (f *cacheFixture) duplicateURL(name string) string {
	return f.server.URL + duplicateRoute + name
}

func (f *cacheFixture) downloads() int64 { return f.requests.Load() }

func fixturePayload(name string) string {
	if name == "empty.jar" {
		return ""
	}
	return "bytes-for-" + name
}

// writeFixtureArtifact names the response after the indexed filename so a test
// can tell the cache's recorded filename apart from the served payload.
func writeFixtureArtifact(w http.ResponseWriter, name, payload string) {
	w.Header().Set(
		"Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", "indexed-"+name),
	)
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = io.WriteString(w, payload)
}

// installNetworkCache points a private cache client at a real cache directory.
func installNetworkCache(t *testing.T, cfg Config) *Client {
	t.Helper()
	return installNetworkCacheAt(t, t.TempDir(), cfg)
}

// installNetworkCacheAt roots the cache client in root, which lets a test
// share one real cache root with helper processes.
func installNetworkCacheAt(t *testing.T, root string, cfg Config) *Client {
	t.Helper()
	client := New(root, cfg, http.DefaultClient, nil)
	h := client.cache()
	if !h.on {
		t.Fatalf("cache handler did not start in %s", h.dir)
	}
	t.Cleanup(func() { _ = h.store.Close() })
	return client
}

// cacheObjects lists the physical objects the cache store holds.
func cacheObjects(t *testing.T, client *Client) []cas.Object {
	t.Helper()
	h := client.cache()
	var objects []cas.Object
	err := h.store.Walk(
		func(object cas.Object) error {
			objects = append(objects, object)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("walk cache store objects in %s: %v", h.dir, err)
	}
	return objects
}

// cacheObjectPaths locates the files backing objects by their digest names.
func cacheObjectPaths(t *testing.T, root string, objects []cas.Object) []string {
	t.Helper()
	wanted := make(map[string]bool, len(objects))
	for _, object := range objects {
		wanted[object.Digest.Hex()] = true
	}
	var paths []string
	err := filepath.WalkDir(
		root,
		func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() || !wanted[entry.Name()] {
				return nil
			}
			if !entry.Type().IsRegular() {
				return fmt.Errorf("unexpected cache file %s", path)
			}
			paths = append(paths, path)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("walk cache root %s: %v", root, err)
	}
	if len(paths) != len(wanted) {
		t.Fatalf(
			"found %d object files in %s, want %d",
			len(paths),
			root,
			len(wanted),
		)
	}
	return paths
}

func requireDirEntries(t *testing.T, dir string, names ...string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	want := append([]string(nil), names...)
	got := make([]string, 0, len(entries))
	for _, entry := range entries {
		got = append(got, entry.Name())
	}
	if len(got) != len(want) {
		t.Fatalf("%s holds %v, want %v", dir, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s holds %v, want %v", dir, got, want)
		}
	}
}

func requireFileBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("%s holds %q, want %q", path, data, want)
	}
}

func digestHex(t *testing.T, algorithm HashAlgorithm, data []byte) string {
	t.Helper()
	switch algorithm {
	case HashSHA1:
		sum := sha1.Sum(data)
		return hex.EncodeToString(sum[:])
	case HashSHA256:
		sum := sha256.Sum256(data)
		return hex.EncodeToString(sum[:])
	case HashSHA512:
		sum := sha512.Sum512(data)
		return hex.EncodeToString(sum[:])
	default:
		t.Fatalf("no digest implementation for %s", algorithm)
		return ""
	}
}

func TestCachedDownloadMissThenHitKeepsIndexedFilename(t *testing.T) {
	fixture := newCacheFixture(t)
	h := installNetworkCache(t, DefaultCacheConfig())
	url := fixture.artifactURL("tool.jar")
	payload := []byte(fixturePayload("tool.jar"))

	first := t.TempDir()
	result, err := h.CachedDownload(
		context.Background(),
		url,
		first,
		DownloadOptions{
			Kind: KindArtifact,
		},
	)
	if err != nil {
		t.Fatalf("first download: %v", err)
	}
	defer result.File.Close()
	if result.CacheHit {
		t.Fatal("first download reported a cache hit")
	}
	if result.Verified {
		t.Fatal("first download reported verification without an expected hash")
	}
	if fixture.downloads() != 1 {
		t.Fatalf("server served %d downloads, want 1", fixture.downloads())
	}
	requireDirEntries(t, first, "indexed-tool.jar")
	requireFileBytes(t, filepath.Join(first, "indexed-tool.jar"), payload)

	entries := h.All()
	if len(entries) != 1 {
		t.Fatalf("index holds %d entries, want 1", len(entries))
	}
	if entries[0].Filename != "indexed-tool.jar" {
		t.Fatalf("index filename %q, want indexed-tool.jar", entries[0].Filename)
	}
	if objects := cacheObjects(t, h); len(objects) != 1 {
		t.Fatalf("cache store holds %d objects, want 1", len(objects))
	}

	second := t.TempDir()
	again, err := h.CachedDownload(
		context.Background(),
		url,
		second,
		DownloadOptions{
			Kind: KindArtifact,
		},
	)
	if err != nil {
		t.Fatalf("second download: %v", err)
	}
	defer again.File.Close()
	if !again.CacheHit {
		t.Fatal("second download reported a cache miss")
	}
	if fixture.downloads() != 1 {
		t.Fatalf(
			"cache hit contacted the server (%d downloads total)",
			fixture.downloads(),
		)
	}
	requireDirEntries(t, second, "indexed-tool.jar")
	requireFileBytes(t, filepath.Join(second, "indexed-tool.jar"), payload)
	if objects := cacheObjects(t, h); len(objects) != 1 {
		t.Fatalf("cache store holds %d objects after the hit, want 1", len(objects))
	}
	if entries := h.All(); len(entries) != 1 ||
		entries[0].Filename != "indexed-tool.jar" {
		t.Fatalf("cache hit changed the index: %+v", entries)
	}
}

// Every stored download rewrites the cache index. A mode an operator set on it
// must survive the rewrite.
func TestCacheIndexRetainsFileMode(t *testing.T) {
	fixture := newCacheFixture(t)
	h := installNetworkCache(t, DefaultCacheConfig())
	indexPath := filepath.Join(h.cache().dir, manifestFilename)

	first, err := h.CachedDownload(
		context.Background(),
		fixture.artifactURL("first.jar"),
		t.TempDir(),
		DownloadOptions{Kind: KindArtifact},
	)
	if err != nil {
		t.Fatalf("first download: %v", err)
	}
	if err := first.File.Close(); err != nil {
		t.Fatalf("close first download: %v", err)
	}
	if err := os.Chmod(indexPath, 0o640); err != nil {
		t.Fatalf("chmod cache index: %v", err)
	}

	second, err := h.CachedDownload(
		context.Background(),
		fixture.artifactURL("second.jar"),
		t.TempDir(),
		DownloadOptions{Kind: KindArtifact},
	)
	if err != nil {
		t.Fatalf("second download: %v", err)
	}
	if err := second.File.Close(); err != nil {
		t.Fatalf("close second download: %v", err)
	}

	info, err := os.Stat(indexPath)
	if err != nil {
		t.Fatalf("stat cache index: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("cache index holds mode %04o, want 0640", got)
	}
	if entries := h.All(); len(entries) != 2 {
		t.Fatalf("index holds %d entries, want the two stored downloads", len(entries))
	}
}

func TestCacheRepairsCorruptObject(t *testing.T) {
	fixture := newCacheFixture(t)
	h := installNetworkCache(t, DefaultCacheConfig())
	url := fixture.artifactURL("tool.jar")
	payload := []byte(fixturePayload("tool.jar"))

	first := t.TempDir()
	result, err := h.CachedDownload(
		context.Background(), url, first, DownloadOptions{Kind: KindArtifact},
	)
	if err != nil {
		t.Fatalf("first download: %v", err)
	}
	_ = result.File.Close()

	paths := cacheObjectPaths(t, h.cache().dir, cacheObjects(t, h))
	if len(paths) != 1 {
		t.Fatalf("found %d object files, want 1", len(paths))
	}
	if err := os.Chmod(paths[0], 0o600); err != nil {
		t.Fatalf("expose cache object %s: %v", paths[0], err)
	}
	if err := os.WriteFile(paths[0], []byte("corrupted cache bytes"), 0o600); err != nil {
		t.Fatalf("corrupt cache object %s: %v", paths[0], err)
	}

	second := t.TempDir()
	again, err := h.CachedDownload(
		context.Background(), url, second, DownloadOptions{Kind: KindArtifact},
	)
	if err != nil {
		t.Fatalf("download after corruption: %v", err)
	}
	_ = again.File.Close()
	if again.CacheHit {
		t.Fatal("corrupt cache bytes were served as a cache hit")
	}
	if fixture.downloads() != 2 {
		t.Fatalf("server served %d downloads, want 2 (one refetch)", fixture.downloads())
	}
	requireDirEntries(t, second, "indexed-tool.jar")
	requireFileBytes(t, filepath.Join(second, "indexed-tool.jar"), payload)

	hit, data, err := h.GetBytes(url)
	if err != nil {
		t.Fatalf("read repaired cache entry: %v", err)
	}
	if !hit || !bytes.Equal(data, payload) {
		t.Fatalf("repaired cache entry holds %q, want %q", data, payload)
	}
	if objects := cacheObjects(t, h); len(objects) != 1 {
		t.Fatalf("cache store holds %d objects after repair, want 1", len(objects))
	}
}

func TestDownloadsSurviveUnavailableCacheStorage(t *testing.T) {
	for _, test := range []struct {
		name     string
		artifact bool
	}{{"artifact", true}, {"metadata", false}} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newCacheFixture(t)
			h := installNetworkCache(t, DefaultCacheConfig())
			temporary := t.TempDir()
			t.Setenv("TMPDIR", temporary)
			t.Setenv("TMP", temporary)
			t.Setenv("TEMP", temporary)
			if err := os.RemoveAll(h.cache().dir); err != nil {
				t.Fatal(err)
			}
			blocked := []byte("cache storage is not a directory\n")
			if err := os.WriteFile(h.cache().dir, blocked, 0o600); err != nil {
				t.Fatal(err)
			}
			if test.artifact {
				destination := t.TempDir()
				payload := []byte(fixturePayload("tool.jar"))
				result, err := h.CachedDownload(
					context.Background(), fixture.artifactURL("tool.jar"), destination, DownloadOptions{
						Kind:          KindArtifact,
						ExpectedHash:  digestHex(t, HashSHA256, payload),
						HashAlgorithm: HashSHA256,
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				if err := result.File.Close(); err != nil {
					t.Fatal(err)
				}
				if result.CacheHit || !result.Verified {
					t.Fatalf("uncached artifact lost verification: %+v", result)
				}
				requireFileBytes(t, filepath.Join(destination, "indexed-tool.jar"), payload)
			} else {
				data, err := h.CachedGetBytes(context.Background(), fixture.artifactURL("metadata.json"), BytesRequestOptions{Kind: KindMetadata})
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(data, []byte(fixturePayload("metadata.json"))) {
					t.Fatalf("metadata bytes changed after storage failure: %q", data)
				}
			}
			if fixture.downloads() != 1 {
				t.Fatal("storage failure repeated the upstream fetch")
			}
			requireFileBytes(t, h.cache().dir, blocked)
			requireDirEntries(t, temporary)
		})
	}
}

func TestCachedDownloadUpstreamMismatchPublishesNothing(t *testing.T) {
	for _, test := range []struct {
		name      string
		algorithm HashAlgorithm
	}{{"sha256", HashSHA256}, {"sha1", HashSHA1}, {"sha512", HashSHA512}} {
		algorithm := test.algorithm
		t.Run(
			test.name, func(t *testing.T) {
				fixture := newCacheFixture(t)
				h := installNetworkCache(t, DefaultCacheConfig())
				url := fixture.artifactURL("tool.jar")
				payload := []byte(fixturePayload("tool.jar"))

				wrong := digestHex(t, algorithm, []byte("unrelated artifact bytes"))
				rejected := t.TempDir()
				_, err := h.CachedDownload(
					context.Background(),
					url,
					rejected,
					DownloadOptions{
						Kind:          KindArtifact,
						ExpectedHash:  wrong,
						HashAlgorithm: algorithm,
					},
				)
				if err == nil {
					t.Fatal("mismatched upstream checksum was accepted")
				}
				requireDirEntries(t, rejected)
				if entries := h.All(); len(entries) != 0 {
					t.Fatalf(
						"index holds %d entries after rejection, want 0",
						len(entries),
					)
				}
				if objects := cacheObjects(t, h); len(objects) != 0 {
					t.Fatalf(
						"store holds %d objects after rejection, want 0",
						len(objects),
					)
				}
				if fixture.downloads() != 1 {
					t.Fatalf(
						"server served %d downloads, want 1",
						fixture.downloads(),
					)
				}

				accepted := t.TempDir()
				expected := digestHex(t, algorithm, payload)
				verified, err := h.CachedDownload(
					context.Background(),
					url,
					accepted,
					DownloadOptions{
						Kind:          KindArtifact,
						ExpectedHash:  expected,
						HashAlgorithm: algorithm,
					},
				)
				if err != nil {
					t.Fatalf("download with the matching checksum: %v", err)
				}
				defer verified.File.Close()
				if !verified.Verified {
					t.Fatal("matching upstream checksum was not reported as verified")
				}
				requireFileBytes(
					t,
					filepath.Join(accepted, "indexed-tool.jar"),
					payload,
				)
				entries := h.All()
				if len(entries) != 1 {
					t.Fatalf("index holds %d entries, want 1", len(entries))
				}
				if entries[0].Integrity.State != IntegrityVerified ||
					entries[0].Integrity.Expected != expected {
					t.Fatalf("index integrity %+v, want verified %s", entries[0].Integrity, expected)
				}
				if objects := cacheObjects(t, h); len(objects) != 1 {
					t.Fatalf("store holds %d objects, want 1", len(objects))
				}
				if fixture.downloads() != 2 {
					t.Fatalf(
						"server served %d downloads, want 2",
						fixture.downloads(),
					)
				}
			},
		)
	}
}

func TestValidateDownloadURL(t *testing.T) {
	t.Parallel()
	for _, value := range []string{
		"http://example.com/artifact.jar",
		"https://example.com/artifact.jar?x=1",
	} {
		if err := validateDownloadURL(value); err != nil {
			t.Fatalf("validateDownloadURL(%q) = %v", value, err)
		}
	}
	for _, value := range []string{
		"",
		"example.com/artifact.jar",
		"file:///tmp/artifact.jar",
		"ftp://example.com/artifact.jar",
		"://",
	} {
		if err := validateDownloadURL(value); err == nil {
			t.Fatalf("validateDownloadURL(%q) accepted a non-HTTP URL", value)
		}
	}
}

func TestReadLimitedBytesEnforcesLimit(t *testing.T) {
	t.Parallel()
	content := []byte("1234")
	tests := []struct {
		name     string
		maximum  int64
		wantData []byte
		wantErr  bool
	}{
		{name: "under limit", maximum: 5, wantData: content},
		{name: "at limit", maximum: 4, wantData: content},
		{name: "over limit", maximum: 3, wantErr: true},
		{name: "unlimited", maximum: 0, wantData: content},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			data, err := readLimitedBytes(bytes.NewReader(content), test.maximum)
			if test.wantErr {
				if err == nil {
					t.Fatalf("readLimitedBytes accepted %d bytes with a %d byte limit", len(content), test.maximum)
				}
				return
			}
			if err != nil {
				t.Fatalf("readLimitedBytes: %v", err)
			}
			if !bytes.Equal(data, test.wantData) {
				t.Fatalf("readLimitedBytes = %q, want %q", data, test.wantData)
			}
		})
	}
}

func TestCachedGetRequestDoesNotCacheNon2xx(t *testing.T) {
	t.Parallel()
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, "missing")
	}))
	t.Cleanup(server.Close)
	client := installNetworkCache(t, DefaultCacheConfig())
	for attempt := range 2 {
		response, err := client.CachedGetRequest(
			context.Background(),
			server.URL+"/missing",
			BytesRequestOptions{Kind: KindMetadata},
		)
		if err != nil {
			t.Fatalf("attempt %d: %v", attempt, err)
		}
		if response.StatusCode != http.StatusNotFound {
			t.Fatalf("attempt %d status = %d, want 404", attempt, response.StatusCode)
		}
		if string(response.Data) != "missing" {
			t.Fatalf("attempt %d data = %q, want missing", attempt, response.Data)
		}
		if response.CacheHit {
			t.Fatalf("attempt %d reported a cache hit for a non-2xx response", attempt)
		}
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("server served %d non-2xx responses, want 2", got)
	}
	if entries := client.All(); len(entries) != 0 {
		t.Fatalf("non-2xx responses produced %d entries", len(entries))
	}
}

func TestCachedGetRequestRejectsOversizeResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "0123456789")
	}))
	t.Cleanup(server.Close)
	client := installNetworkCache(t, DefaultCacheConfig())
	if _, err := client.CachedGetRequest(
		context.Background(),
		server.URL+"/large",
		BytesRequestOptions{Kind: KindMetadata, MaxBytes: 5},
	); err == nil {
		t.Fatal("oversize response was accepted")
	}
	if entries := client.All(); len(entries) != 0 {
		t.Fatalf("oversize response produced %d entries", len(entries))
	}
}

func TestCachedGetBytesRejectsNon2xx(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "failure")
	}))
	t.Cleanup(server.Close)
	client := installNetworkCache(t, DefaultCacheConfig())
	if _, err := client.CachedGetBytes(
		context.Background(),
		server.URL+"/failure",
		BytesRequestOptions{Kind: KindMetadata},
	); err == nil {
		t.Fatal("CachedGetBytes accepted a 500 response")
	}
}

func TestCachedGetRequestHonorsPreCanceledContext(t *testing.T) {
	t.Parallel()
	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "unexpected")
	}))
	t.Cleanup(server.Close)
	client := installNetworkCache(t, DefaultCacheConfig())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.CachedGetRequest(
		ctx,
		server.URL+"/artifact",
		BytesRequestOptions{Kind: KindMetadata},
	); !errors.Is(err, context.Canceled) {
		t.Fatalf("CachedGetRequest with a canceled context = %v", err)
	}
	if got := hits.Load(); got != 0 {
		t.Fatalf("canceled request reached the server %d times", got)
	}
}

func TestNewIntegrityHasherSelectsAlgorithm(t *testing.T) {
	t.Parallel()
	if hasher := newIntegrityHasher(HashNone, "digest"); hasher != nil {
		t.Fatal("HashNone selected a hasher")
	}
	if hasher := newIntegrityHasher(HashSHA256, "digest"); hasher != nil {
		t.Fatal("SHA-256 reused a separate hasher")
	}
	if hasher := newIntegrityHasher(HashSHA1, ""); hasher != nil {
		t.Fatal("empty expected hash selected a hasher")
	}
	if hasher := newIntegrityHasher(HashSHA1, "digest"); hasher == nil ||
		hasher.Size() != sha1.Size {
		t.Fatal("SHA-1 did not select a SHA-1 hasher")
	}
	if hasher := newIntegrityHasher(HashSHA512, "digest"); hasher == nil ||
		hasher.Size() != sha512.Size {
		t.Fatal("SHA-512 did not select a SHA-512 hasher")
	}
}
