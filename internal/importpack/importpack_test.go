package importpack

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	for _, rel := range []string{"lucy.yaml", "LUCY-LOCK.YAML", "Lucy.Yaml"} {
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
			_, err := Materialize(context.Background(), path, dest, nil)
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
	result, err := Materialize(context.Background(), path, dest, nil)
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

func TestMaterializeTarball(t *testing.T) {
	path := writeTarball(t, map[string]string{
		"root/server.properties": "online-mode=false\n",
		"root/mods/a.jar":        "jar",
	})
	dest := t.TempDir()
	result, err := Materialize(context.Background(), path, dest, nil)
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
	result, err := Materialize(context.Background(), path, dest, nil)
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
