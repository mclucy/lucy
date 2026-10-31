package cmd

import (
	"os"
	"testing"

	"github.com/mclucy/lucy/catalog"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/server"
	"gopkg.in/yaml.v3"
)

// TestActivityGuardBlocksSharedReplays runs one workspace whose
// native server reports itself as running while an installation journal is
// still pending. Every entry point that replays workspace state must refuse to
// change anything, and the first mutation after the server stops must replay
// the journal before it touches a declaration.
func TestActivityGuardBlocksSharedReplays(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	core := miniVanillaJar(t, "1.21.4")
	w.write(t, "server/server.jar", core, 0o644)
	w.write(t, "server/server.properties", []byte("level-name=world\n"), 0o644)
	lock := &documentpkg.Lock{
		SchemaVersion:       documentpkg.LockSchemaVersion,
		ManifestFingerprint: "sha256:" + digestHex([]byte("a manifest fingerprint")),
		Server: documentpkg.LockedServer{
			Minecraft:   "1.21.4",
			Directory:   "server",
			Product:     server.ProductMinecraft,
			CoreVersion: "1.21.4",
			Artifact: catalog.Artifact{
				Provider: catalog.ProviderMojang,
				Version:  "1.21.4",
				Filename: "server.jar",
				URL:      "https://example.invalid/server.jar",
				Hashes:   map[string]string{"sha256": digestHex(core)},
			},
			Java: documentpkg.LockedJava{Advisory: 21},
		},
		Packages: []documentpkg.Package{},
	}
	w.writeLock(t, lock)

	backup := "rollback-activity"
	beforeImage := []byte("journal before-image\n")
	backupObject := ".lucy/" + backup + "/" + digestHex(beforeImage)
	w.write(t, backupObject, beforeImage, 0o644)
	beforeInfo, err := os.Stat(w.path(backupObject))
	if err != nil {
		t.Fatalf("stat fixture pre-image: %v", err)
	}
	working := []byte("working bytes\n")
	w.write(t, "config.txt", working, 0o644)
	workingInfo, err := os.Stat(w.path("config.txt"))
	if err != nil {
		t.Fatalf("stat fixture working file: %v", err)
	}
	journal, err := yaml.Marshal(workspacewrite.InstallJournal{
		Backup: ".lucy/" + backup,
		Entries: []workspacewrite.InstallJournalEntry{{
			Path: "config.txt",
			Before: &workspacewrite.InstallImage{
				SHA256: digestHex(beforeImage),
				Mode:   uint32(beforeInfo.Mode().Perm()),
			},
			After: &workspacewrite.InstallImage{
				SHA256: digestHex(working),
				Mode:   uint32(workingInfo.Mode().Perm()),
			},
		}},
	})
	if err != nil {
		t.Fatalf("marshal fixture journal: %v", err)
	}
	w.write(t, ".lucy/commit.yaml", journal, 0o600)

	hold := w.holdWorldSession(t, "server")
	manifestBytes := w.read(t, documentpkg.ManifestFilename)
	lockBytes := w.read(t, documentpkg.LockFilename)
	journalBytes := w.read(t, ".lucy/commit.yaml")

	for _, args := range [][]string{
		{"install"},
		{"create", "--force", "--minecraft", "1.21.4", "--core", "vanilla", "--core-version", "1.21.4", "--no-install"},
		{"add", "modrinth:sodium"},
	} {
		w.lucyFail(t, args...)
	}
	w.write(t, "notes.txt", []byte("a note\n"), 0o644)
	w.lucyFail(t, "file", "track", "notes.txt")

	w.requireBytes(t, "config.txt", working)
	w.requireBytes(t, ".lucy/commit.yaml", journalBytes)
	w.requireBytes(t, backupObject, beforeImage)
	w.requireBytes(t, documentpkg.ManifestFilename, manifestBytes)
	w.requireBytes(t, documentpkg.LockFilename, lockBytes)
	w.requireAbsent(t, ".lucy/history/files.jsonl")

	hold.release()

	w.lucyOK(t, "file", "track", "notes.txt")
	w.requireBytes(t, "config.txt", beforeImage)
	w.requireAbsent(t, ".lucy/commit.yaml")
	w.requireAbsent(t, ".lucy/"+backup)
	if status := w.statusOf(t, "notes.txt"); status.State != "matching" {
		t.Errorf("the recovered mutation left an unexpected state: %+v", status)
	}
}

func TestRunningServerBlocksOverwriteWithoutJournal(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	w.write(t, "server/server.jar", miniVanillaJar(t, "1.21.4"), 0o644)
	w.write(t, "server/server.properties", []byte("level-name=world\n"), 0o644)
	accepted := []byte("accepted rules\n")
	replacement := []byte("replacement rules\n")
	source := newSourceServer(t, replacement)
	w.write(t, "config/rules.txt", accepted, 0o644)
	w.fileResult(t, "track", "config/rules.txt", "--recover-url", source.url("rules.txt"))
	drifted := []byte("working drift\n")
	w.write(t, "config/rules.txt", drifted, 0o644)
	w.requireAbsent(t, ".lucy/commit.yaml")
	w.requireAbsent(t, ".lucy/file-commit.json")
	manifestBytes := w.read(t, documentpkg.ManifestFilename)
	historyBytes := w.read(t, ".lucy/history/files.jsonl")
	hold := w.holdWorldSession(t, "server")

	w.lucyFail(t, "file", "recover", "config/rules.txt")
	w.lucyFail(t, "file", "update", "config/rules.txt", "--from-source")
	w.requireBytes(t, "config/rules.txt", drifted)
	w.requireBytes(t, documentpkg.ManifestFilename, manifestBytes)
	w.requireBytes(t, ".lucy/history/files.jsonl", historyBytes)
	if source.hits() != 0 {
		t.Fatal("running-server rejection fetched a recovery source")
	}
	hold.release()

	w.lucyOK(t, "file", "recover", "config/rules.txt")
	w.requireBytes(t, "config/rules.txt", accepted)
	w.lucyOK(t, "file", "update", "config/rules.txt", "--from-source")
	w.requireBytes(t, "config/rules.txt", replacement)
	if status := w.statusOf(t, "config/rules.txt"); status.Hash != acceptedHash(replacement) || status.State != "matching" {
		t.Fatalf("file writer did not resume after server exit: %+v", status)
	}
}

func TestStaleLockRuntimeBlocksFileWritesWhileActive(t *testing.T) {
	w := newWorkspace(t)
	document := baseManifest(t)
	document.Server.Directory = "server-manifest"
	fingerprint := w.writeManifest(t, document)
	core := miniVanillaJar(t, "1.21.4")
	accepted := []byte("accepted rules\n")
	revised := []byte("revised rules\n")
	replacement := []byte("replacement rules\n")
	source := newSourceServer(t, replacement)
	w.write(t, "config/rules.txt", accepted, 0o644)
	lock := &documentpkg.Lock{
		SchemaVersion:       documentpkg.LockSchemaVersion,
		ManifestFingerprint: fingerprint,
		Server: documentpkg.LockedServer{
			Minecraft:   "1.21.4",
			Directory:   "server-lock",
			Product:     server.ProductMinecraft,
			CoreVersion: "1.21.4",
			Artifact: catalog.Artifact{
				Provider: catalog.ProviderMojang,
				Version:  "1.21.4",
				Filename: "server.jar",
				URL:      "https://example.invalid/server.jar",
				Hashes:   map[string]string{"sha256": digestHex(core)},
			},
			Java: documentpkg.LockedJava{Advisory: 21},
		},
		Packages: []documentpkg.Package{},
	}
	w.writeLock(t, lock)
	w.fileResult(t, "track", "config/rules.txt", "--recover-url", source.url("rules.txt"))
	w.write(t, "config/rules.txt", revised, 0o644)
	w.fileResult(t, "update", "config/rules.txt")
	drifted := []byte("working drift\n")
	w.write(t, "config/rules.txt", drifted, 0o644)
	w.write(t, "server-lock/server.jar", core, 0o644)
	w.write(t, "server-lock/server.properties", []byte("level-name=world\n"), 0o644)
	stale := w.readLock(t)
	stale.ManifestFingerprint = fingerprint
	w.writeLock(t, stale)
	w.requireAbsent(t, ".lucy/commit.yaml")
	w.requireAbsent(t, ".lucy/file-commit.json")

	hold := w.holdWorldSession(t, "server-lock")
	manifestBytes := w.read(t, documentpkg.ManifestFilename)
	lockBytes := w.read(t, documentpkg.LockFilename)
	historyBytes := w.read(t, ".lucy/history/files.jsonl")

	w.lucyFail(t, "file", "recover", "config/rules.txt")
	w.lucyFail(t, "file", "revert", "config/rules.txt")
	w.lucyFail(t, "file", "update", "config/rules.txt", "--from-source")
	w.requireBytes(t, "config/rules.txt", drifted)
	w.requireBytes(t, documentpkg.ManifestFilename, manifestBytes)
	w.requireBytes(t, documentpkg.LockFilename, lockBytes)
	w.requireBytes(t, ".lucy/history/files.jsonl", historyBytes)
	if source.hits() != 0 {
		t.Fatal("stale-lock rejection fetched a recovery source")
	}

	hold.release()

	w.lucyOK(t, "file", "revert", "config/rules.txt")
	w.requireBytes(t, "config/rules.txt", accepted)
	w.lucyOK(t, "file", "update", "config/rules.txt", "--from-source")
	w.requireBytes(t, "config/rules.txt", replacement)
	if status := w.statusOf(t, "config/rules.txt"); status.Hash != acceptedHash(replacement) || status.State != "matching" {
		t.Fatalf("file writer did not resume after the stale-lock runtime stopped: %+v", status)
	}
}
