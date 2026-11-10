package operation

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	logpkg "github.com/mclucy/lucy/log"
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
