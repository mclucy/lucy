//go:build integration

package filetrack_test

import (
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/server"
	"gopkg.in/yaml.v3"
)

// TestWorkspaceActivityGuardBlocksSharedReplayEntries runs one workspace whose
// native server reports itself as running while an installation journal is
// still pending. Every entry point that replays workspace state must refuse to
// change anything, and the first mutation after the server stops must replay
// the journal before it touches a declaration.
func TestWorkspaceActivityGuardBlocksSharedReplayEntries(t *testing.T) {
	w := newWorkspace(t)
	w.writeManifest(t, baseManifest(t))
	core := miniVanillaJar(t, "1.21.4")
	w.write(t, "server/server.jar", core, 0o644)
	w.write(t, "server/server.properties", []byte("level-name=world\n"), 0o644)
	lock := &lockfile.Document{
		SchemaVersion:       lockfile.SchemaVersion,
		ManifestFingerprint: "sha256:" + digestHex([]byte("a manifest fingerprint")),
		Server: lockfile.Server{
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
			Java: lockfile.Java{Advisory: 21},
		},
		Packages: []lockfile.Package{},
	}
	w.writeLock(t, lock)

	backup := "rollback-activity"
	beforeImage := []byte("journal before-image\n")
	w.write(t, ".lucy/"+backup+"/config.txt", beforeImage, 0o644)
	working := []byte("working bytes\n")
	w.write(t, "config.txt", working, 0o644)
	journal, err := yaml.Marshal(struct {
		Backup  string   `yaml:"backup"`
		Changed []string `yaml:"changed"`
		Created []string `yaml:"created"`
	}{
		Backup:  w.path(".lucy", backup),
		Changed: []string{"config.txt"},
		Created: []string{},
	})
	if err != nil {
		t.Fatalf("marshal fixture journal: %v", err)
	}
	w.write(t, ".lucy/commit.yaml", journal, 0o600)

	hold := w.holdWorldSession(t, "server")
	manifestBytes := w.read(t, manifest.Filename)
	lockBytes := w.read(t, lockfile.Filename)
	journalBytes := w.read(t, ".lucy/commit.yaml")

	for _, args := range [][]string{
		{"install"},
		{"create", "--force", "--minecraft", "1.21.4", "--core", "vanilla", "--core-version", "1.21.4", "--directory", "server"},
		{"add", "modrinth:sodium"},
	} {
		w.lucyFail(t, args...)
	}
	w.write(t, "notes.txt", []byte("a note\n"), 0o644)
	w.lucyFail(t, "file", "track", "notes.txt")

	w.requireBytes(t, "config.txt", working)
	w.requireBytes(t, ".lucy/commit.yaml", journalBytes)
	w.requireBytes(t, ".lucy/"+backup+"/config.txt", beforeImage)
	w.requireBytes(t, manifest.Filename, manifestBytes)
	w.requireBytes(t, lockfile.Filename, lockBytes)
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

func TestRunningServerBlocksFileOverwriteWithoutPendingJournal(t *testing.T) {
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
	manifestBytes := w.read(t, manifest.Filename)
	historyBytes := w.read(t, ".lucy/history/files.jsonl")
	hold := w.holdWorldSession(t, "server")

	w.lucyFail(t, "file", "recover", "config/rules.txt")
	w.lucyFail(t, "file", "update", "config/rules.txt", "--from-source")
	w.requireBytes(t, "config/rules.txt", drifted)
	w.requireBytes(t, manifest.Filename, manifestBytes)
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
	lock := &lockfile.Document{
		SchemaVersion:       lockfile.SchemaVersion,
		ManifestFingerprint: fingerprint,
		Server: lockfile.Server{
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
			Java: lockfile.Java{Advisory: 21},
		},
		Packages: []lockfile.Package{},
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
	manifestBytes := w.read(t, manifest.Filename)
	lockBytes := w.read(t, lockfile.Filename)
	historyBytes := w.read(t, ".lucy/history/files.jsonl")

	w.lucyFail(t, "file", "recover", "config/rules.txt")
	w.lucyFail(t, "file", "revert", "config/rules.txt")
	w.lucyFail(t, "file", "update", "config/rules.txt", "--from-source")
	w.requireBytes(t, "config/rules.txt", drifted)
	w.requireBytes(t, manifest.Filename, manifestBytes)
	w.requireBytes(t, lockfile.Filename, lockBytes)
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
