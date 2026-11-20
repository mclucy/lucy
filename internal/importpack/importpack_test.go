package importpack

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/server"
)

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for name, content := range files {
		entry, err := writer.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeTarball(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	writer := tar.NewWriter(gz)
	for name, content := range files {
		if err := writer.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestInspectMrpackMapsServerFilesAndLoader(t *testing.T) {
	index := `{
  "formatVersion": 1,
  "game": "minecraft",
  "versionId": "1.2.3",
  "name": "Example Pack",
  "files": [
    {"path":"mods/server.jar","hashes":{"sha512":"00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"},"env":{"client":"required","server":"required"},"downloads":["https://example.invalid/server.jar"],"fileSize":10},
    {"path":"mods/optional.jar","hashes":{"sha512":"00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"},"env":{"client":"required","server":"optional"},"downloads":["https://example.invalid/optional.jar"],"fileSize":20},
    {"path":"mods/client.jar","hashes":{"sha512":"00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"},"env":{"client":"required","server":"unsupported"},"downloads":[],"fileSize":30}
  ],
  "dependencies": {"minecraft":"1.21.4","fabric-loader":"0.16.9"}
}`
	path := writeZip(t, map[string]string{"modrinth.index.json": index})
	plan, err := Inspect(context.Background(), path)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if plan.Format != FormatMrpack || plan.Minecraft != "1.21.4" ||
		plan.Product != server.ProductFabric || plan.CoreVersion != "0.16.9" {
		t.Fatalf("plan = %+v", plan)
	}
	if plan.ServerFiles != 2 || plan.SkippedClient != 1 || plan.Bytes != 30 {
		t.Fatalf("counts = %+v", plan)
	}
}

func TestInspectRejectsClientPackAndLauncher(t *testing.T) {
	t.Run("client pack", func(t *testing.T) {
		path := writeZip(t, map[string]string{
			"manifest.json":      `{"manifestType":"minecraftModpack"}`,
			"overrides/config/a": "a",
			"overrides/config/b": "b",
		})
		_, err := Inspect(context.Background(), path)
		var formatErr *FormatError
		if !asFormatError(err, &formatErr) || !strings.Contains(formatErr.Kind, "client") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("launcher", func(t *testing.T) {
		path := writeZip(t, map[string]string{"mmc-pack.json": "{}", "minecraft/mods/a.jar": "x"})
		_, err := Inspect(context.Background(), path)
		var formatErr *FormatError
		if !asFormatError(err, &formatErr) || !strings.Contains(formatErr.Kind, "launcher") {
			t.Fatalf("err = %v", err)
		}
	})
}

func TestCheckDestPathRejectsWorkspaceDocuments(t *testing.T) {
	for _, rel := range []string{
		"lucy.yaml",
		"LUCY-LOCK.YAML",
		"Lucy.Yaml",
		"lucy.yaml/child",
		"Lucy-Lock.Yaml/child",
	} {
		err := checkDestPath(rel)
		if !errors.Is(err, ErrUnsafeEntry) {
			t.Errorf("checkDestPath(%q) = %v, want ErrUnsafeEntry", rel, err)
		}
	}
	for _, rel := range []string{
		"backup/lucy.yaml",
		"mods/lucy-lock.yaml",
		"lucy.yaml.txt",
	} {
		if err := checkDestPath(rel); err != nil {
			t.Errorf("checkDestPath(%q) = %v, want nil", rel, err)
		}
	}
}

func TestInspectRejectsWorkspaceDocumentAtRoot(t *testing.T) {
	path := writeZip(t, map[string]string{
		"lucy.yaml":         "name: server\n",
		"server.properties": "level-name=world\n",
	})
	_, err := Inspect(t.Context(), path)
	if !errors.Is(err, ErrUnsafeEntry) {
		t.Fatalf("Inspect error = %v, want ErrUnsafeEntry", err)
	}
}

func TestMaterializeTreeRejectsTraversalAndCaseCollision(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  error
	}{
		{"traversal", map[string]string{"../evil": "x"}, ErrUnsafeEntry},
		{"absolute", map[string]string{"/etc/evil": "x"}, ErrUnsafeEntry},
		{"collision", map[string]string{"mods/A.jar": "a", "mods/a.jar": "b"}, ErrUnsafeEntry},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeZip(t, tt.files)
			dest := t.TempDir()
			_, err := Materialize(context.Background(), path, dest, nil, Progress{})
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want error matching %v", err, tt.want)
			}
			if _, statErr := os.Stat(filepath.Join(filepath.Dir(dest), "evil")); statErr == nil {
				t.Fatal("traversal wrote outside destination")
			}
		})
	}
}

func TestMaterializeTreeFlattensSingleRoot(t *testing.T) {
	path := writeZip(t, map[string]string{
		"pack/server.properties": "online-mode=false\n",
		"pack/mods/example.jar":  "jar",
	})
	dest := t.TempDir()
	result, err := Materialize(context.Background(), path, dest, nil, Progress{})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if result.Format != FormatTree || result.ServerFiles != 2 {
		t.Fatalf("result = %+v", result)
	}
	if _, err := os.Stat(filepath.Join(dest, "server.properties")); err != nil {
		t.Fatalf("flattened server.properties: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "pack")); !os.IsNotExist(err) {
		t.Fatalf("wrapper directory remains: %v", err)
	}
}

// TestMaterializeKeepsServerRootWrapper checks that a lone top-level folder
// that is itself a server root keeps its name, while a folder that only holds
// a server root is removed.
func TestMaterializeKeepsServerRootWrapper(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  []string
		gone  []string
	}{
		{
			name:  "mods folder of jars",
			files: map[string]string{"mods/a.jar": "a", "mods/b.jar": "b"},
			want:  []string{"mods/a.jar", "mods/b.jar"},
			gone:  []string{"a.jar"},
		},
		{
			name:  "plugins folder of jars",
			files: map[string]string{"plugins/a.jar": "a"},
			want:  []string{"plugins/a.jar"},
			gone:  []string{"a.jar"},
		},
		{
			name: "config wrapper holding a server root",
			files: map[string]string{
				"config/server.properties": "online-mode=false\n",
				"config/mods/a.jar":        "a",
			},
			want: []string{"server.properties", "mods/a.jar"},
			gone: []string{"config/server.properties"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dest := t.TempDir()
			result, err := Materialize(
				context.Background(),
				writeZip(t, tt.files),
				dest,
				nil,
				Progress{},
			)
			if err != nil {
				t.Fatalf("Materialize: %v", err)
			}
			if result.Format != FormatTree {
				t.Fatalf("Format = %q, want %q", result.Format, FormatTree)
			}
			for _, rel := range tt.want {
				if _, err := os.Stat(filepath.Join(dest, filepath.FromSlash(rel))); err != nil {
					t.Errorf("%s: %v", rel, err)
				}
			}
			for _, rel := range tt.gone {
				if _, err := os.Stat(filepath.Join(dest, filepath.FromSlash(rel))); !os.IsNotExist(err) {
					t.Errorf("%s should not exist", rel)
				}
			}
		})
	}
}

func TestMaterializeTarball(t *testing.T) {
	path := writeTarball(t, map[string]string{
		"root/server.properties": "online-mode=false\n",
		"root/mods/a.jar":        "jar",
	})
	dest := t.TempDir()
	result, err := Materialize(context.Background(), path, dest, nil, Progress{})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if result.ServerFiles != 2 {
		t.Fatalf("ServerFiles = %d, want 2", result.ServerFiles)
	}
	if _, err := os.Stat(filepath.Join(dest, "server.properties")); err != nil {
		t.Fatal(err)
	}
}

func TestMaterializeMrpackSkipsClientAndAppliesServerOverrides(t *testing.T) {
	index := `{
  "formatVersion": 1,
  "game": "minecraft",
  "versionId": "1",
  "name": "Pack",
  "files": [
    {"path":"mods/client.jar","hashes":{"sha512":"00000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000"},"env":{"client":"required","server":"unsupported"},"downloads":[]}
  ],
  "dependencies": {"minecraft":"1.21.4","neoforge":"21.1.80"}
}`
	path := writeZip(t, map[string]string{
		"modrinth.index.json":                  index,
		"overrides/config/example.toml":        "shared",
		"server-overrides/config/example.toml": "server",
	})
	dest := t.TempDir()
	result, err := Materialize(context.Background(), path, dest, nil, Progress{})
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}
	if result.Product != server.ProductNeoForge || result.CoreVersion != "21.1.80" {
		t.Fatalf("plan = %+v", result.Plan)
	}
	if len(result.Skipped) != 1 || result.Skipped[0] != "mods/client.jar" {
		t.Fatalf("skipped = %v", result.Skipped)
	}
	data, err := os.ReadFile(filepath.Join(dest, "config", "example.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "server" {
		t.Fatalf("override = %q, want server", data)
	}
	if result.Overrides != 1 {
		t.Fatalf("overrides = %d, want 1", result.Overrides)
	}
}

func TestInspectRejectsPlainText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Inspect(context.Background(), path)
	var formatErr *FormatError
	if !asFormatError(err, &formatErr) {
		t.Fatalf("err = %v, want FormatError", err)
	}
}

func asFormatError(err error, target **FormatError) bool {
	for err != nil {
		if typed, ok := err.(*FormatError); ok {
			*target = typed
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}
	return false
}

// openTarTest opens path as a tar.gz archive with the given aggregate budget,
// bypassing openArchive so a test can set a budget below the production limit.
func openTarTest(t *testing.T, path string, budget int64) *archive {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return &archive{path: path, container: containerTarGzip, file: file, budget: budget}
}

// writeRawTarGzip writes a tar.gz stream built by fn and leaves the tar stream
// unterminated, so a header can declare a body the stream never carries.
func writeRawTarGzip(t *testing.T, fn func(tw *tar.Writer)) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source.tar.gz")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(file)
	fn(tar.NewWriter(gz))
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTarScanStopsAtAggregateBudget(t *testing.T) {
	const budget = 1 << 20
	const bodySize = 600 << 10
	path := writeRawTarGzip(t, func(tw *tar.Writer) {
		if err := tw.WriteHeader(&tar.Header{Name: "a.jar", Mode: 0o644, Size: bodySize}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(strings.Repeat("a", bodySize))); err != nil {
			t.Fatal(err)
		}
		// b.jar declares a body the stream never carries. The budget must reject
		// its header before the scan reads or drains that body.
		if err := tw.WriteHeader(&tar.Header{Name: "b.jar", Mode: 0o644, Size: bodySize}); err != nil {
			t.Fatal(err)
		}
	})
	a := openTarTest(t, path, budget)
	if err := a.scan(t.Context()); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("scan error = %v, want ErrLimitExceeded", err)
	}
}

func TestTarScanRejectsOversizedDeclaredEntry(t *testing.T) {
	path := writeRawTarGzip(t, func(tw *tar.Writer) {
		if err := tw.WriteHeader(&tar.Header{Name: "huge.jar", Mode: 0o644, Size: maxFileBytes + 1}); err != nil {
			t.Fatal(err)
		}
	})
	a := openTarTest(t, path, maxExtractedBytes)
	if err := a.scan(t.Context()); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("scan error = %v, want ErrLimitExceeded", err)
	}
}

func TestTarScanHonorsContextCancellation(t *testing.T) {
	path := writeTarball(t, map[string]string{"a.txt": "a", "b.txt": "b"})

	cancelled, cancelNow := context.WithCancel(t.Context())
	cancelNow()
	if _, err := openArchive(cancelled, path); !errors.Is(err, context.Canceled) {
		t.Fatalf("openArchive error = %v, want context.Canceled", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a := openTarTest(t, path, maxExtractedBytes)
	calls := 0
	err := a.visit(ctx, false, func(_ int, _ visited, _ io.Reader) error {
		calls++
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("visit error = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("visit callbacks = %d, want 1 after cancellation", calls)
	}
}

func newImportCache(t *testing.T) *cache.Client {
	t.Helper()
	client := cache.New(
		filepath.Join(t.TempDir(), "cache"),
		cache.DefaultCacheConfig(),
		http.DefaultClient,
		nil,
	)
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// mrpackDownload returns an index download that expects payload's SHA-512.
func mrpackDownload(rel string, payload []byte, urls ...string) *item {
	sum := sha512.Sum512(payload)
	return &item{
		rel:  rel,
		mode: 0o644,
		download: &mrpackFile{
			urls:      urls,
			algorithm: cache.HashSHA512,
			digest:    hex.EncodeToString(sum[:]),
		},
	}
}

func TestMrpackFailingDownloadDoesNotStarveAnother(t *testing.T) {
	for _, mode := range []string{"content length", "streaming", "cache hit"} {
		t.Run(mode, func(t *testing.T) {
			const budget = 1000
			payload := []byte(strings.Repeat("g", 600))
			started := make(chan struct{})
			finish := make(chan struct{})
			release := sync.OnceFunc(func() { close(finish) })
			defer release()
			var requests atomic.Int64
			mux := http.NewServeMux()
			mux.HandleFunc("/truncated", func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", strconv.Itoa(len(payload)+1))
				_, _ = w.Write(payload)
				if err := http.NewResponseController(w).Flush(); err != nil {
					t.Errorf("flush partial response: %v", err)
					return
				}
				select {
				case <-finish:
				case <-r.Context().Done():
				}
			})
			mux.HandleFunc("/good", func(w http.ResponseWriter, _ *http.Request) {
				requests.Add(1)
				if mode != "streaming" {
					w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
				} else if err := http.NewResponseController(w).Flush(); err != nil {
					t.Errorf("flush response headers: %v", err)
					return
				}
				_, _ = w.Write(payload)
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			client := newImportCache(t)
			good := mrpackDownload("mods/good.jar", payload, srv.URL+"/good")
			if mode == "cache hit" {
				warm := &materializer{network: client, budget: &byteBudget{left: budget}}
				if _, err := warm.downloadFrom(ctx, good.download.urls[0], good, t.TempDir()); err != nil {
					t.Fatalf("warm cache: %v", err)
				}
			}
			bad := mrpackDownload("mods/truncated.jar", payload, srv.URL+"/truncated")
			m := &materializer{
				network: client,
				budget:  &byteBudget{left: budget},
				progress: Progress{FileProgress: func(name string, read, _ int64) {
					if name == bad.rel && read == int64(len(payload)) {
						close(started)
					}
				}},
			}
			failed := make(chan error, 1)
			badDir := t.TempDir()
			go func() {
				_, err := m.downloadFrom(ctx, bad.download.urls[0], bad, badDir)
				failed <- err
			}()
			select {
			case <-started:
			case err := <-failed:
				t.Fatalf("first request finished before streaming: %v", err)
			case <-ctx.Done():
				t.Fatal("first response did not stream before the deadline")
			}

			path, err := m.downloadFrom(ctx, good.download.urls[0], good, t.TempDir())
			if err != nil {
				t.Fatalf("second download failed while the first attempt was in flight: %v", err)
			}
			release()
			if err := <-failed; !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("first download error = %v, want unexpected EOF", err)
			}
			data, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(data, payload) {
				t.Fatalf("retained file has %d bytes, want %d verified bytes (read error: %v)", len(data), len(payload), err)
			}
			if got := m.budget.total(); got != int64(len(payload)) {
				t.Fatalf("aggregate retained %d bytes, want %d", got, len(payload))
			}
			if got := requests.Load(); got != 1 {
				t.Fatalf("requests for the good file = %d, want 1", got)
			}
		})
	}
}

// TestMrpackDownloadStillEnforcesAggregate checks that charging on retention
// keeps the shared limit: a file that does not fit after earlier files is
// rejected and leaves nothing behind.
func TestMrpackDownloadStillEnforcesAggregate(t *testing.T) {
	payload := []byte(strings.Repeat("a", 600))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(len(payload)))
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	client := newImportCache(t)
	download := mrpackDownload("mods/too-much.jar", payload, srv.URL+"/payload")
	m := &materializer{
		network: client,
		budget:  &byteBudget{left: int64(len(payload)) - 1},
	}
	dir := t.TempDir()
	if _, err := m.downloadFrom(t.Context(), srv.URL+"/payload", download, dir); !errors.Is(err, ErrLimitExceeded) {
		t.Fatalf("download error = %v, want ErrLimitExceeded", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("staged files after a rejected file = %v", entries)
	}
}

func TestFetchTriesAnotherMirrorAfterAnOversizedResponse(t *testing.T) {
	var mirrorHits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oversized":
			w.Header().Set("Content-Length", strconv.FormatInt(maxFileBytes+1, 10))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("partial"))
		case "/mirror":
			mirrorHits.Add(1)
			_, _ = w.Write([]byte("x"))
		}
	}))
	defer srv.Close()

	m, err := newMaterializer(t.Context(), t.TempDir(), newImportCache(t), Progress{})
	if err != nil {
		t.Fatal(err)
	}
	defer m.cleanup()
	if err := m.ensureStage(); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(m.dest, "mods"), dirMode); err != nil {
		t.Fatal(err)
	}
	download := mrpackDownload(
		"mods/mirror.jar",
		[]byte("x"),
		srv.URL+"/oversized",
		srv.URL+"/mirror",
	)
	if err := m.fetch(t.Context(), download, 0); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(m.dest, download.rel))
	if err != nil || string(data) != "x" || mirrorHits.Load() != 1 {
		t.Fatalf("verified mirror file = %q, error = %v, mirror requests = %d", data, err, mirrorHits.Load())
	}
}
