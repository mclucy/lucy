//go:build integration

package filetrack_test

import (
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/server"
)

func TestFileTrackRejectsForbiddenAndNonRegularPaths(t *testing.T) {
	outside := filepath.Join(t.TempDir(), "outside.txt")
	if err := writeFileBytes(outside, []byte("outside the workspace\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ name, path string }{
		{"absolute", outside},
		{"backslash", `config\rules.txt`},
		{"private state", ".lucy/state.txt"},
		{"git state", ".git/config"},
		{"manifest", "lucy.yaml"},
		{"lock", "lucy-lock.yaml"},
		{"manifest descendant", "lucy.yaml/config.txt"},
		{"lock descendant", "lucy-lock.yaml/config.txt"},
		{"root directory", "."},
		{"directory", "server"},
	} {
		t.Run(test.name, func(t *testing.T) {
			w := newWorkspace(t)
			w.writeManifest(t, baseManifest(t))
			w.write(t, "config/retained.txt", []byte("retained\n"), 0o644)
			if err := mkdirIfMissing(w.path("server")); err != nil {
				t.Fatal(err)
			}
			before := w.read(t, document.ManifestFilename)
			w.lucyFail(t, "file", "track", test.path)
			w.requireBytes(t, document.ManifestFilename, before)
			w.requireBytes(t, "config/retained.txt", []byte("retained\n"))
			w.requireAbsent(t, ".lucy/history/files.jsonl")
		})
	}
}

func TestFileTrackRejectsTraversalToExistingFile(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	outside := []byte("outside the workspace\n")
	w.write(t, "../outside.txt", outside, 0o644)
	w.requireBytes(t, "../outside.txt", outside)
	before := w.read(t, document.ManifestFilename)

	w.lucyFail(t, "file", "track", "../outside.txt")

	w.requireBytes(t, "../outside.txt", outside)
	w.requireBytes(t, document.ManifestFilename, before)
	w.requireAbsent(t, ".lucy/history/files.jsonl")
}

func TestFileTrackRejectsSymlinkedPaths(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	outsideDirectory := filepath.Join(t.TempDir(), "outside")
	if err := writeFileBytes(filepath.Join(outsideDirectory, "inside.txt"), []byte("outside\n"), 0o644); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	w.symlink(t, filepath.Join(outsideDirectory, "inside.txt"), "config/linked.txt")
	w.symlink(t, outsideDirectory, "outside")
	w.write(t, "real/inside.txt", []byte("inside\n"), 0o644)
	w.symlink(t, w.path("real"), "internal")

	w.lucyFail(t, "file", "track", "config/linked.txt")
	w.lucyFail(t, "file", "track", "outside/inside.txt")
	w.lucyFail(t, "file", "track", "internal/inside.txt")
	w.requireBytes(t, "real/inside.txt", []byte("inside\n"))
	w.requireBytes(t, "outside/inside.txt", []byte("outside\n"))
	if files := w.readManifest(t).Files; len(files) != 0 {
		t.Errorf("symlinked paths left %d declarations behind", len(files))
	}
}

func TestFileListReportsSymlinkedDeclarationAsError(t *testing.T) {
	w := newWorkspace(t)
	outsideDirectory := filepath.Join(t.TempDir(), "outside")
	inside := []byte("outside\n")
	if err := writeFileBytes(filepath.Join(outsideDirectory, "inside.txt"), inside, 0o644); err != nil {
		t.Fatalf("write symlink target: %v", err)
	}
	w.symlink(t, outsideDirectory, "outside")
	w.writeManifest(t, &document.Manifest{
		SchemaVersion: document.ManifestSchemaVersion,
		Server: document.ManifestServer{
			Minecraft: "1.21.4",
			Directory: "server",
			Core:      document.ManifestCore{Product: server.ProductMinecraft},
		},
		Files: []document.TrackedFile{manifestTrackedFile("outside/inside.txt", acceptedHash(inside))},
	})

	status := w.statusOf(t, "outside/inside.txt")
	if status.State != "error" {
		t.Errorf("symlinked declaration is not an error state: %+v", status)
	}
}

func TestFileManifestRejectsDuplicateNormalizedPaths(t *testing.T) {
	sameShape := "manifest_version: 1\nserver:\n  minecraft: 1.21.4\n  directory: server\n  core:\n    distribution: vanilla\nfiles:\n"
	duplicateHash := acceptedHash([]byte("shared\n"))

	w := newWorkspace(t)
	w.write(t, document.ManifestFilename, []byte(sameShape+
		"  - path: ./config/a.txt\n    hash: "+duplicateHash+"\n"+
		"  - path: config/a.txt\n    hash: "+duplicateHash+"\n"), 0o644)
	before := w.read(t, document.ManifestFilename)
	w.lucyFail(t, "file", "list")
	w.lucyFail(t, "file", "track", "config/b.txt")
	w.requireBytes(t, document.ManifestFilename, before)
	w.requireAbsent(t, ".lucy/history/files.jsonl")

	casing := newWorkspace(t)
	casing.write(t, document.ManifestFilename, []byte(sameShape+
		"  - path: config/A.txt\n    hash: "+duplicateHash+"\n"+
		"  - path: config/a.txt\n    hash: "+duplicateHash+"\n"), 0o644)
	casing.lucyFail(t, "file", "list")
	casing.requireAbsent(t, ".lucy")
}

func TestFileManifestRejectsImmutableWithoutHash(t *testing.T) {
	w := newWorkspace(t)
	w.write(t, document.ManifestFilename, []byte(
		"manifest_version: 1\nserver:\n  minecraft: 1.21.4\n  directory: server\n  core:\n    distribution: vanilla\nfiles:\n"+
			"  - path: config/rules.txt\n    immutable: true\n"), 0o644)
	w.lucyFail(t, "file", "list")
	w.requireAbsent(t, ".lucy")
}

func TestFileTrackRejectsConflictingChoices(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	path := "config/rules.txt"
	w.write(t, path, []byte("rules\n"), 0o644)

	w.lucyFail(t, "file", "track", path, "--no-hash", "--immutable")
	w.lucyFail(t, "file", "track", path, "--no-hash", "--recover-url", "http://127.0.0.1:9/rules.txt")
	w.lucyFail(t, "file", "track", path, "--recover-url", "not-a-url")
	w.lucyFail(t, "file", "track", path, "--recover-url", "ftp://127.0.0.1/rules.txt")
	w.requireBytes(t, path, []byte("rules\n"))
	w.requireAbsent(t, ".lucy/history/files.jsonl")
	w.lucyOK(t, "file", "track", path)
	beforeManifest := w.read(t, document.ManifestFilename)
	beforeHistory := w.read(t, ".lucy/history/files.jsonl")
	w.lucyFail(t, "file", "update", path, "--from-source")
	w.lucyFail(t, "file", "update", "--all", path)
	w.lucyFail(t, "file", "recover", "--all", path)
	w.lucyFail(t, "file", "update")
	w.requireBytes(t, document.ManifestFilename, beforeManifest)
	w.requireBytes(t, ".lucy/history/files.jsonl", beforeHistory)
	w.requireBytes(t, path, []byte("rules\n"))
	if files := w.readManifest(t).Files; len(files) != 1 || files[0].Hash == "" {
		t.Errorf("rejected choices disturbed the tracking declaration: %+v", files)
	}
}
