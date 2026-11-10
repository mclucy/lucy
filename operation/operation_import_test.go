package operation

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	logpkg "github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/upstream"
)

// writeImportTreeZip packages a synthetic server directory with a vanilla jar,
// server properties, EULA and operator list as a tree archive.
func writeImportTreeZip(t *testing.T) string {
	t.Helper()
	srcDir := t.TempDir()
	writeVanillaTestJar(t, srcDir, "server.jar", "1.21.4")
	files := map[string]string{
		"server.properties": "online-mode=false\nenable-rcon=true\n",
		"eula.txt":          "eula=true\n",
		"ops.json":          `[{"uuid":"00000000-0000-0000-0000-000000000001","name":"Steve","level":4}]`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(srcDir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return zipImportTree(t, srcDir)
}

// zipImportTree packs every regular file under srcDir into a tree archive.
func zipImportTree(t *testing.T, srcDir string) string {
	t.Helper()
	zipPath := filepath.Join(t.TempDir(), "server.zip")
	out, err := os.Create(zipPath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	defer out.Close()
	writer := zip.NewWriter(out)
	err = filepath.WalkDir(srcDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		rel, err := filepath.Rel(srcDir, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		w, err := writer.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		_, err = w.Write(data)
		return err
	})
	if err != nil {
		t.Fatalf("walk source: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return zipPath
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// assertNoImportStage fails when any staging leftover remains in dir.
func assertNoImportStage(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".lucy-import-") {
			t.Fatalf("staging leftover %s in %s", entry.Name(), dir)
		}
	}
}

func TestImportTreeArchivePublishesWorkspace(t *testing.T) {
	isolateOperationHome(t)
	service := newInitializeTestService(t)
	zipPath := writeImportTreeZip(t)
	target := filepath.Join(t.TempDir(), "imported")

	result, err := service.Import(t.Context(), ImportOptions{
		Source:    zipPath,
		Directory: target,
		SHA256:    fileSHA256(t, zipPath),
	}, nil)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Format != "tree" {
		t.Fatalf("Format = %q, want tree", result.Format)
	}
	if result.Minecraft != "1.21.4" {
		t.Fatalf("Minecraft = %q, want 1.21.4", result.Minecraft)
	}
	for _, name := range []string{documentpkg.ManifestFilename, documentpkg.LockFilename} {
		if _, err := os.Stat(filepath.Join(target, name)); err != nil {
			t.Fatalf("published target missing %s: %v", name, err)
		}
	}
	assertNoImportStage(t, filepath.Dir(target))

	if strings.Contains(fmt.Sprintf("%+v", result), ".lucy-import-") {
		t.Fatalf("result leaks staging path: %+v", result)
	}

	entries, err := service.registry.List()
	if err != nil {
		t.Fatalf("registry list: %v", err)
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatalf("resolve target: %v", err)
	}
	registeredTarget := false
	for _, entry := range entries {
		if strings.Contains(entry.Path, ".lucy-import-") {
			t.Fatalf("registry holds staging path %q", entry.Path)
		}
		if entry.Path == resolvedTarget || entry.Path == target {
			registeredTarget = true
		}
	}
	if !registeredTarget {
		t.Fatalf("registry has no entry for published target %s", resolvedTarget)
	}
}

func TestImportLeavesTargetUntouchedOnFailure(t *testing.T) {
	isolateOperationHome(t)
	service := newInitializeTestService(t)
	zipPath := writeImportTreeZip(t)

	textPath := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(textPath, []byte("not a server"), 0o644); err != nil {
		t.Fatalf("write text source: %v", err)
	}

	tests := []struct {
		name string
		opts func(target string) ImportOptions
	}{
		{
			name: "missing source",
			opts: func(target string) ImportOptions {
				return ImportOptions{Source: filepath.Join(t.TempDir(), "missing.zip"), Directory: target}
			},
		},
		{
			name: "sha256 mismatch",
			opts: func(target string) ImportOptions {
				return ImportOptions{Source: zipPath, Directory: target, SHA256: strings.Repeat("0", 64)}
			},
		},
		{
			name: "unsupported format",
			opts: func(target string) ImportOptions {
				return ImportOptions{Source: textPath, Directory: target}
			},
		},
		{
			name: "non-http scheme",
			opts: func(target string) ImportOptions {
				return ImportOptions{Source: "ftp://example.com/server.zip", Directory: target}
			},
		},
		{
			name: "relative target",
			opts: func(target string) ImportOptions {
				return ImportOptions{Source: zipPath, Directory: filepath.Base(target)}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := t.TempDir()
			target := filepath.Join(parent, "imported")
			if _, err := service.Import(t.Context(), tt.opts(target), nil); err == nil {
				t.Fatal("Import succeeded, want error")
			}
			if _, err := os.Stat(target); !os.IsNotExist(err) {
				t.Fatalf("target exists after failure: stat err = %v", err)
			}
			assertNoImportStage(t, parent)
		})
	}
}

func TestImportRejectsNonEmptyTarget(t *testing.T) {
	isolateOperationHome(t)
	service := newInitializeTestService(t)
	zipPath := writeImportTreeZip(t)
	target := filepath.Join(t.TempDir(), "imported")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}
	keep := filepath.Join(target, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o644); err != nil {
		t.Fatalf("write keep: %v", err)
	}

	if _, err := service.Import(t.Context(), ImportOptions{
		Source:    zipPath,
		Directory: target,
	}, nil); err == nil {
		t.Fatal("Import into non-empty target succeeded, want error")
	}
	data, err := os.ReadFile(keep)
	if err != nil {
		t.Fatalf("read keep: %v", err)
	}
	if string(data) != "keep" {
		t.Fatalf("keep.txt = %q, want original content", data)
	}
	if _, err := os.Stat(filepath.Join(target, documentpkg.ManifestFilename)); !os.IsNotExist(err) {
		t.Fatalf("manifest written into rejected target: stat err = %v", err)
	}
	assertNoImportStage(t, filepath.Dir(target))
}

func TestPublishImportStageRollsBackAndRejectsForeignTarget(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	if err := os.Mkdir(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "server.properties"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "target")

	publication, err := publishImportStage(stage, target)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "server.properties")); err != nil {
		t.Fatalf("target file: %v", err)
	}
	if err := publication.rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stage, "server.properties")); err != nil {
		t.Fatalf("stage file after rollback: %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target survives rollback: %v", err)
	}

	foreign := filepath.Join(parent, "foreign")
	if err := os.WriteFile(foreign, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := publishImportStage(stage, foreign); err == nil {
		t.Fatal("publish over foreign file succeeded")
	}
	data, err := os.ReadFile(foreign)
	if err != nil || string(data) != "keep" {
		t.Fatalf("foreign file changed: %q, %v", data, err)
	}
}

func TestPublishImportStageKeepsEmptyTargetInode(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	if err := os.Mkdir(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "server.properties"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}

	publication, err := publishImportStage(stage, target)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	after, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target after publish: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("publication replaced the empty target directory")
	}
	if _, err := os.Stat(filepath.Join(target, "server.properties")); err != nil {
		t.Fatalf("target file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stage, "server.properties")); !os.IsNotExist(err) {
		t.Fatalf("stage file survives publication: %v", err)
	}

	if err := publication.rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if _, err := os.Stat(filepath.Join(stage, "server.properties")); err != nil {
		t.Fatalf("stage file after rollback: %v", err)
	}
	restored, err := os.Stat(target)
	if err != nil {
		t.Fatalf("target after rollback: %v", err)
	}
	if !os.SameFile(before, restored) {
		t.Fatal("rollback replaced the empty target directory")
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("target holds %d entries after rollback, want 0", len(entries))
	}
}

func TestPublishImportStageRejectsNonEmptyDirectoryTarget(t *testing.T) {
	t.Parallel()

	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	if err := os.Mkdir(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stage, "server.properties"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	keep := filepath.Join(target, "keep.txt")
	if err := os.WriteFile(keep, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := publishImportStage(stage, target); err == nil {
		t.Fatal("publish into non-empty directory succeeded")
	}
	if data, err := os.ReadFile(keep); err != nil || string(data) != "keep" {
		t.Fatalf("keep.txt changed: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(stage, "server.properties")); err != nil {
		t.Fatalf("stage file moved by rejected publication: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, "server.properties")); !os.IsNotExist(err) {
		t.Fatalf("stage file leaked into rejected target: %v", err)
	}
}

// An imported package with a readable descriptor but no matching release is
// left unmanaged by adoption, so it must appear in the unmanaged report even
// though discovery identified it.
func TestImportReportsDescriptorPackageWithoutReleaseAsUnmanaged(t *testing.T) {
	isolateOperationHome(t)
	service := newInitializeTestService(t)
	srcDir := t.TempDir()
	writeFabricTestLauncher(t, srcDir, "0.19.5", "1.21.1")
	writeInitializeTestJar(t, filepath.Join(srcDir, "mods", "example.jar"), map[string]string{
		"fabric.mod.json": `{"id":"example","version":"1.0.0"}`,
	})
	target := filepath.Join(t.TempDir(), "imported")

	result, err := service.Import(t.Context(), ImportOptions{
		Source:    zipImportTree(t, srcDir),
		Directory: target,
	}, nil)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !slices.Contains(result.Unmanaged, "mods/example.jar") {
		t.Fatalf("Unmanaged = %v, want mods/example.jar", result.Unmanaged)
	}
}

// Publication must keep the writer lock that registration depends on: the
// renamed stage carries its held lock into the target before any entry is
// visible, so a concurrent operation cannot acquire the workspace first.
func TestPublishImportStageRetainsWriterLockUntilRelease(t *testing.T) {
	parent := t.TempDir()
	stage := filepath.Join(parent, "stage")
	if err := os.Mkdir(stage, 0o755); err != nil {
		t.Fatal(err)
	}
	logger, err := logpkg.New(filepath.Join(parent, "lucy.log"), "debug", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer logger.Close()
	writer, err := workspacewrite.Lock(stage, logger)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "target")

	publication, err := publishImportStage(stage, target)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if _, err := workspacewrite.Lock(target, logger); err == nil {
		t.Fatal("published target accepted a second writer lock")
	} else if !strings.Contains(err.Error(), "another Lucy operation") {
		t.Fatalf("second lock error = %v, want an owned-workspace error", err)
	}
	if err := publication.rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target survives rollback: %v", err)
	}
}

func TestCreateImportParentsDoesNotRemoveDanglingSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	link := filepath.Join(root, "mirror")
	if err := os.Symlink(filepath.Join(root, "missing"), link); err != nil {
		t.Fatal(err)
	}
	if _, err := createImportParents(filepath.Join(link, "server")); err == nil {
		t.Fatal("createImportParents accepted a dangling symlink parent")
	}
	if _, err := os.Lstat(link); err != nil {
		t.Fatalf("dangling symlink was removed: %v", err)
	}
}

// fakePackResolver stands in for the router's pack resolution. It embeds the
// service's real source so the rest of the Source contract keeps working.
type fakePackResolver struct {
	Source
	pack  upstream.ImportPack
	err   error
	calls []string
}

func (f *fakePackResolver) ResolveImportPack(
	_ context.Context,
	provider catalog.Provider,
	project, version string,
) (upstream.ImportPack, error) {
	f.calls = append(f.calls, fmt.Sprintf("%s:%s@%s", provider, project, version))
	return f.pack, f.err
}

// newRemoteImportTestService installs resolver in place of the test source.
func newRemoteImportTestService(t *testing.T, resolver *fakePackResolver) *Service {
	t.Helper()
	service := newInitializeTestService(t)
	resolver.Source = service.source
	service.source = resolver
	return service
}

// newRemoteImportNetwork returns a real cache client for download tests.
func newRemoteImportNetwork(t *testing.T) *cache.Client {
	t.Helper()
	return cache.New(t.TempDir(), cache.DefaultCacheConfig(), http.DefaultClient, nil)
}

// serveImportPack serves data at /files/server.zip for the test's lifetime.
func serveImportPack(t *testing.T, data []byte) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(data)
	}))
	t.Cleanup(server.Close)
	return server.URL + "/files/server.zip"
}

func TestParseRemoteImportSource(t *testing.T) {
	tests := []struct {
		name    string
		source  string
		want    RemoteImportSource
		ok      bool
		wantErr bool
	}{
		{
			name:   "modrinth slug",
			source: "modrinth:all-the-mods",
			want:   RemoteImportSource{Provider: catalog.ProviderModrinth, Project: "all-the-mods"},
			ok:     true,
		},
		{
			name:   "modrinth version pin",
			source: "modrinth:all-the-mods@1.2.0",
			want: RemoteImportSource{
				Provider: catalog.ProviderModrinth,
				Project:  "all-the-mods",
				Version:  "1.2.0",
			},
			ok: true,
		},
		{
			name:   "curseforge slug",
			source: "curseforge:all-the-mods",
			want:   RemoteImportSource{Provider: catalog.ProviderCurseForge, Project: "all-the-mods"},
			ok:     true,
		},
		{
			name:   "curseforge numeric id with file pin",
			source: "curseforge:123456@4567890",
			want: RemoteImportSource{
				Provider: catalog.ProviderCurseForge,
				Project:  "123456",
				Version:  "4567890",
			},
			ok: true,
		},
		{
			name:   "modrinth page",
			source: "https://modrinth.com/modpack/all-the-mods",
			want:   RemoteImportSource{Provider: catalog.ProviderModrinth, Project: "all-the-mods"},
			ok:     true,
		},
		{
			name:   "modrinth page with version",
			source: "https://modrinth.com/modpack/all-the-mods/version/1.2.0",
			want: RemoteImportSource{
				Provider: catalog.ProviderModrinth,
				Project:  "all-the-mods",
				Version:  "1.2.0",
			},
			ok: true,
		},
		{
			name:   "curseforge page",
			source: "https://www.curseforge.com/minecraft/modpacks/all-the-mods",
			want:   RemoteImportSource{Provider: catalog.ProviderCurseForge, Project: "all-the-mods"},
			ok:     true,
		},
		{
			name:   "curseforge page with file",
			source: "https://www.curseforge.com/minecraft/modpacks/all-the-mods/files/4567890",
			want: RemoteImportSource{
				Provider: catalog.ProviderCurseForge,
				Project:  "all-the-mods",
				Version:  "4567890",
			},
			ok: true,
		},
		{name: "modrinth empty version", source: "modrinth:all-the-mods@", ok: true, wantErr: true},
		{name: "modrinth missing slug", source: "modrinth:", ok: true, wantErr: true},
		{name: "curseforge non-numeric file", source: "curseforge:all-the-mods@latest", ok: true, wantErr: true},
		{
			name:    "curseforge file page with non-numeric file",
			source:  "https://www.curseforge.com/minecraft/modpacks/all-the-mods/files/abc",
			ok:      true,
			wantErr: true,
		},
		{
			name:    "modrinth page with unknown subpath",
			source:  "https://modrinth.com/modpack/all-the-mods/versions",
			ok:      true,
			wantErr: true,
		},
		{name: "direct archive URL", source: "https://example.com/packs/server.zip"},
		{name: "unrecognized host stays direct", source: "https://example.org/modpack/all-the-mods"},
		{name: "http modrinth page stays direct", source: "http://modrinth.com/modpack/all-the-mods"},
		{name: "local path", source: "./server.zip"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok, err := ParseRemoteImportSource(tt.source)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseRemoteImportSource(%q) error = %v, wantErr %v", tt.source, err, tt.wantErr)
			}
			if ok != tt.ok {
				t.Fatalf("ParseRemoteImportSource(%q) ok = %v, want %v", tt.source, ok, tt.ok)
			}
			if got != tt.want {
				t.Fatalf("ParseRemoteImportSource(%q) = %+v, want %+v", tt.source, got, tt.want)
			}
		})
	}
}

func TestImportRemoteSourceFailuresLeaveTargetUntouched(t *testing.T) {
	tests := []struct {
		name     string
		resolver *fakePackResolver
		wantErr  string
	}{
		{
			name:     "resolver error",
			resolver: &fakePackResolver{err: errors.New("project not found")},
			wantErr:  "project not found",
		},
		{
			name:     "empty download URL",
			resolver: &fakePackResolver{pack: upstream.ImportPack{Filename: "server.zip"}},
			wantErr:  "no download URL",
		},
		{
			name: "ftp scheme",
			resolver: &fakePackResolver{pack: upstream.ImportPack{
				URL:      "ftp://example.com/server.zip",
				Filename: "server.zip",
			}},
			wantErr: "not http or https",
		},
		{
			name: "missing host",
			resolver: &fakePackResolver{pack: upstream.ImportPack{
				URL:      "https:///server.zip",
				Filename: "server.zip",
			}},
			wantErr: "no host",
		},
		{
			name: "empty filename",
			resolver: &fakePackResolver{pack: upstream.ImportPack{
				URL: "https://example.com/server.zip",
			}},
			wantErr: "no filename",
		},
		{
			name: "filename is root",
			resolver: &fakePackResolver{pack: upstream.ImportPack{
				URL:      "https://example.com/server.zip",
				Filename: "/",
			}},
			wantErr: "not a file name",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := newRemoteImportTestService(t, tt.resolver)
			parent := t.TempDir()
			target := filepath.Join(parent, "imported")
			_, err := service.Import(
				t.Context(),
				ImportOptions{Source: "modrinth:all-the-mods", Directory: target},
				newRemoteImportNetwork(t),
			)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Import error = %v, want error containing %q", err, tt.wantErr)
			}
			if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
				t.Fatalf("target exists after failed import: %v", statErr)
			}
			assertNoImportStage(t, parent)
		})
	}
}

func TestImportRemoteSourceNeedsResolver(t *testing.T) {
	service := newInitializeTestService(t)
	parent := t.TempDir()
	target := filepath.Join(parent, "imported")
	_, err := service.Import(
		t.Context(),
		ImportOptions{Source: "curseforge:all-the-mods", Directory: target},
		newRemoteImportNetwork(t),
	)
	if err == nil || !strings.Contains(err.Error(), "no curseforge pack resolver is configured") {
		t.Fatalf("Import error = %v, want missing resolver error", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("target exists after failed import: %v", statErr)
	}
	assertNoImportStage(t, parent)
}

func TestImportRemoteSourceDownloadsResolvedPack(t *testing.T) {
	data, err := os.ReadFile(writeImportTreeZip(t))
	if err != nil {
		t.Fatalf("read pack: %v", err)
	}
	packURL := serveImportPack(t, data)
	digest := sha512.Sum512(data)
	resolver := &fakePackResolver{pack: upstream.ImportPack{
		Provider: catalog.ProviderModrinth,
		Project:  "all-the-mods",
		Version:  "1.2.0",
		Name:     "All The Mods",
		Filename: "server.zip",
		URL:      packURL,
		// sha512 must win over the deliberately wrong sha1.
		Hashes: map[string]string{
			"sha512": hex.EncodeToString(digest[:]),
			"sha1":   strings.Repeat("0", 40),
		},
	}}
	service := newRemoteImportTestService(t, resolver)
	target := filepath.Join(t.TempDir(), "imported")
	result, err := service.Import(
		t.Context(),
		ImportOptions{Source: "modrinth:all-the-mods@1.2.0", Directory: target},
		newRemoteImportNetwork(t),
	)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Source != "modrinth:all-the-mods@1.2.0" {
		t.Fatalf("result.Source = %q, want the user identifier", result.Source)
	}
	if want := []string{"modrinth:all-the-mods@1.2.0"}; !slices.Equal(resolver.calls, want) {
		t.Fatalf("resolver calls = %v, want %v", resolver.calls, want)
	}
}

func TestImportRemotePackVerificationFailuresLeaveTargetUntouched(t *testing.T) {
	data, err := os.ReadFile(writeImportTreeZip(t))
	if err != nil {
		t.Fatalf("read pack: %v", err)
	}
	packURL := serveImportPack(t, data)
	tests := []struct {
		name   string
		hashes map[string]string
		sha256 string
	}{
		{
			name:   "pack sha512 mismatch",
			hashes: map[string]string{"sha512": strings.Repeat("0", 128)},
		},
		{
			name:   "--sha256 mismatch",
			sha256: strings.Repeat("0", 64),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := &fakePackResolver{pack: upstream.ImportPack{
				Filename: "server.zip",
				URL:      packURL,
				Hashes:   tt.hashes,
			}}
			service := newRemoteImportTestService(t, resolver)
			parent := t.TempDir()
			target := filepath.Join(parent, "imported")
			if _, err := service.Import(
				t.Context(),
				ImportOptions{
					Source:    "modrinth:all-the-mods",
					Directory: target,
					SHA256:    tt.sha256,
				},
				newRemoteImportNetwork(t),
			); err == nil {
				t.Fatal("Import succeeded, want verification error")
			}
			if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
				t.Fatalf("target exists after failed import: %v", statErr)
			}
			assertNoImportStage(t, parent)
		})
	}
}

func TestImportDirectURLBypassesRemoteResolver(t *testing.T) {
	data, err := os.ReadFile(writeImportTreeZip(t))
	if err != nil {
		t.Fatalf("read pack: %v", err)
	}
	directURL := serveImportPack(t, data)
	resolver := &fakePackResolver{}
	service := newRemoteImportTestService(t, resolver)
	target := filepath.Join(t.TempDir(), "imported")
	result, err := service.Import(
		t.Context(),
		ImportOptions{Source: directURL, Directory: target},
		newRemoteImportNetwork(t),
	)
	if err != nil {
		t.Fatalf("Import direct URL: %v", err)
	}
	if result.Source != directURL {
		t.Fatalf("result.Source = %q, want %q", result.Source, directURL)
	}
	if len(resolver.calls) != 0 {
		t.Fatalf("resolver called for direct URL: %v", resolver.calls)
	}
}
