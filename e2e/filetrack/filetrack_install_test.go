//go:build integration

package filetrack_test

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/server"
)

func TestInstallPreservesRuntimePinsAndCoreBytes(t *testing.T) {
	w := newWorkspace(t)
	_, lock, core := w.seedNativeRuntime(t)
	rules := []byte("server rules v1\n")
	w.write(t, "config/rules.txt", rules, 0o644)
	coreMtime := w.modTime(t, "server/server.jar")

	w.lucyOK(t, "file", "track", "config/rules.txt")
	w.lucyOK(t, "file", "lock", "config/rules.txt")
	if !w.modTime(t, "server/server.jar").Equal(coreMtime) {
		t.Error("a file command touched the installed core")
	}
	w.requireBytes(t, "server/server.jar", core)

	fresh := w.readLock(t)
	if fresh.ManifestFingerprint != w.manifestFingerprint(t) {
		t.Errorf("the lock is not current after the file change: %s", fresh.ManifestFingerprint)
	}
	if !reflect.DeepEqual(fresh.Server, lock.Server) {
		t.Errorf("a file change rewrote the runtime pins:\n%+v\n%+v", fresh.Server, lock.Server)
	}
	if len(fresh.Files) != 1 || fresh.Files[0].Path != "config/rules.txt" || !fresh.Files[0].Immutable {
		t.Errorf("the lock did not publish the declaration: %+v", fresh.Files)
	}

	manifestBytes := w.read(t, document.ManifestFilename)
	lockBytes := w.read(t, document.LockFilename)
	receipt := w.readReceipt(t)
	if len(receipt.Entries) != 1 || receipt.Entries[0].Path != "server/server.jar" {
		t.Fatalf("unexpected fixture receipt %+v", receipt)
	}

	w.lucyOK(t, "install")
	if !w.modTime(t, "server/server.jar").Equal(coreMtime) {
		t.Error("a no-op install touched the installed core")
	}
	w.requireBytes(t, "server/server.jar", core)
	w.requireBytes(t, document.LockFilename, lockBytes)
	w.requireBytes(t, document.ManifestFilename, manifestBytes)
	w.requireReceiptEntries(t, receiptEntry{Path: "server/server.jar", SHA256: digestHex(core), Mode: 0o644})

	// The file commands rewrite the manifest and the lock. The permissions a
	// user chose for them must survive that rewrite.
	w.chmod(t, document.ManifestFilename, 0o640)
	w.chmod(t, document.LockFilename, 0o640)

	// A manifest-only files edit republishes the lock without reinstalling,
	// because the receipt never measured the tracked files.
	w.write(t, "config/extra.txt", []byte("extra note\n"), 0o644)
	w.lucyOK(t, "file", "track", "config/extra.txt")
	w.requireMode(t, document.ManifestFilename, 0o640)
	w.requireMode(t, document.LockFilename, 0o640)
	fresh = w.readLock(t)
	if len(fresh.Files) != 2 || fresh.ManifestFingerprint != w.manifestFingerprint(t) {
		t.Errorf("the manifest-only edit did not publish a current lock: %+v", fresh.Files)
	}
	if !reflect.DeepEqual(fresh.Server, lock.Server) {
		t.Error("a manifest-only edit rewrote the runtime pins")
	}
	w.lucyOK(t, "install")
	if !w.modTime(t, "server/server.jar").Equal(coreMtime) {
		t.Error("a manifest-only edit forced a reinstall")
	}
	w.requireReceiptEntries(t, receiptEntry{Path: "server/server.jar", SHA256: digestHex(core), Mode: 0o644})

	w.lucyOK(t, "install", "--offline")
	if !w.modTime(t, "server/server.jar").Equal(coreMtime) {
		t.Error("an offline no-op install touched the installed core")
	}
}

func TestInstallProvisionsMissingFileFromCAS(t *testing.T) {
	w := newWorkspace(t)
	_, _, core := w.seedNativeRuntime(t)
	rules := []byte("server rules v2\n")
	w.write(t, "config/rules.txt", rules, 0o644)
	w.lucyOK(t, "file", "track", "config/rules.txt")
	coreMtime := w.modTime(t, "server/server.jar")

	for _, offline := range []bool{false, true} {
		if err := os.Remove(w.path("config/rules.txt")); err != nil {
			t.Fatalf("remove the tracked file: %v", err)
		}
		args := []string{"install"}
		if offline {
			args = append(args, "--offline")
		}
		w.lucyOK(t, args...)
		w.requireBytes(t, "config/rules.txt", rules)
		if !w.modTime(t, "server/server.jar").Equal(coreMtime) {
			t.Fatal("provisioning touched the installed core")
		}
		w.requireBytes(t, "server/server.jar", core)
		if status := w.statusOf(t, "config/rules.txt"); status.State != "matching" {
			t.Errorf("provisioned file is not the accepted revision: %+v", status)
		}
	}
}

func TestFileInstallWarnsImmutableDriftWithoutRestoring(t *testing.T) {
	w := newWorkspace(t)
	w.seedNativeRuntime(t)
	rules := []byte("server rules v3\n")
	w.write(t, "config/rules.txt", rules, 0o644)
	w.lucyOK(t, "file", "track", "config/rules.txt")
	w.lucyOK(t, "file", "lock", "config/rules.txt")
	coreMtime := w.modTime(t, "server/server.jar")
	drifted := []byte("drifted rules v3\n")
	w.write(t, "config/rules.txt", drifted, 0o644)

	run := w.lucyOK(t, "install", "--json")
	result := decodeJSON[install.Result](t, run.stdout)
	warned := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Path == "config/rules.txt" {
			warned = true
			break
		}
	}
	if !warned {
		t.Errorf("install omitted the immutable deviation diagnostic: %+v", result.Diagnostics)
	}
	w.requireBytes(t, "config/rules.txt", drifted)
	if !w.modTime(t, "server/server.jar").Equal(coreMtime) {
		t.Error("the warned install touched the installed core")
	}

	status := w.statusOf(t, "config/rules.txt")
	if status.State != "modified" || !status.Immutable {
		t.Errorf("install changed the reported drift state: %+v", status)
	}
}

func TestFileStaleLockStaysStaleAfterFileOnlyChange(t *testing.T) {
	w := newWorkspace(t)
	w.seedNativeRuntime(t)
	rules := []byte("server rules v4\n")
	w.write(t, "config/rules.txt", rules, 0o644)
	w.lucyOK(t, "file", "track", "config/rules.txt")

	stale := w.readLock(t)
	stale.ManifestFingerprint = "sha256:" + strings.Repeat("a", 64)
	w.writeLock(t, stale)
	pins := w.readLock(t).Server
	receiptBytes := w.read(t, ".lucy/install.yaml")

	w.lucyOK(t, "file", "lock", "config/rules.txt")
	fresh := w.readLock(t)
	if fresh.ManifestFingerprint != "sha256:"+strings.Repeat("a", 64) {
		t.Errorf("a file-only change stamped a fresh fingerprint onto a stale lock: %s", fresh.ManifestFingerprint)
	}
	if len(fresh.Files) != 1 || fresh.Files[0].Path != "config/rules.txt" || !fresh.Files[0].Immutable {
		t.Errorf("the file-only change lost the declaration: %+v", fresh.Files)
	}
	if !reflect.DeepEqual(fresh.Server, pins) {
		t.Error("the file-only change rewrote the runtime pins")
	}
	w.requireBytes(t, ".lucy/install.yaml", receiptBytes)

	w.lucyFail(t, "install", "--offline")
	w.lucyFail(t, "install", "--locked")
	w.requireBytes(t, ".lucy/install.yaml", receiptBytes)
}

func TestFileLockOnlyWorkspaceServesReadOnlyCommands(t *testing.T) {
	w := newWorkspace(t)
	core := []byte("installed core bytes\n")
	w.write(t, "server/server.jar", core, 0o644)
	rules := []byte("rules v1\n")
	w.write(t, "config/rules.txt", rules, 0o644)
	lock := &document.Lock{
		SchemaVersion:       document.LockSchemaVersion,
		ManifestFingerprint: "sha256:" + digestHex([]byte("a manifest that is gone")),
		Server: document.LockedServer{
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
			Java: document.LockedJava{Advisory: 21},
		},
		Packages: []document.Package{},
		Files:    []document.TrackedFile{manifestTrackedFile("config/rules.txt", acceptedHash(rules))},
	}
	w.writeLock(t, lock)
	w.requireAbsent(t, document.ManifestFilename)

	if status := w.statusOf(t, "config/rules.txt"); status.State != "matching" || status.Hash != acceptedHash(rules) {
		t.Errorf("the lock-only workspace does not replay the declaration: %+v", status)
	}
	w.requireAbsent(t, ".lucy")
	if events := w.lucyHistory(t, ""); len(events) != 0 {
		t.Errorf("lock-only history invented events: %+v", events)
	}

	drifted := []byte("drifted rules\n")
	w.write(t, "config/rules.txt", drifted, 0o644)
	if status := w.statusOf(t, "config/rules.txt"); status.State != "modified" || status.ActualHash != acceptedHash(drifted) {
		t.Errorf("the lock-only workspace lost the drift: %+v", status)
	}
}

func TestFileTrackRejectsCoreAndReceiptCollisions(t *testing.T) {
	t.Run("generated receipt output", func(t *testing.T) {
		w := newWorkspace(t)
		w.writeManifest(t, baseManifest(t))
		notes := []byte("generated note\n")
		w.write(t, "generated/notes.txt", notes, 0o644)
		w.writeReceipt(t, &document.Lock{}, receiptEntry{Path: "generated/notes.txt", SHA256: digestHex(notes), Mode: 0o644})

		w.lucyFail(t, "file", "track", "generated/notes.txt")
		w.requireBytes(t, "generated/notes.txt", notes)
		if files := w.readManifest(t).Files; len(files) != 0 {
			t.Errorf("the collision still published a declaration: %+v", files)
		}
	})

	t.Run("managed core", func(t *testing.T) {
		w := newWorkspace(t)
		_, _, core := w.seedNativeRuntime(t)
		w.lucyFail(t, "file", "track", "server/server.jar")
		w.requireBytes(t, "server/server.jar", core)
		if files := w.readManifest(t).Files; len(files) != 0 {
			t.Errorf("the collision still published a declaration: %+v", files)
		}
	})

	t.Run("locked package output", func(t *testing.T) {
		w := newWorkspace(t)
		_, nativeLock, _ := w.seedNativeRuntime(t)
		jar := []byte("package jar bytes\n")
		w.write(t, "server/mods/sodium.jar", jar, 0o644)
		lock := &document.Lock{
			SchemaVersion:       document.LockSchemaVersion,
			ManifestFingerprint: "sha256:" + digestHex([]byte("stale")),
			Server:              nativeLock.Server,
			Packages: []document.Package{{
				Environment: server.EnvironmentServer,
				Ecosystem:   catalog.EcosystemFabric,
				Artifact: catalog.Artifact{
					Provider: catalog.ProviderModrinth,
					Version:  "0.6.0",
					Filename: "sodium.jar",
					URL:      "https://example.invalid/sodium.jar",
					Hashes:   map[string]string{"sha256": digestHex(jar)},
				},
			}},
		}
		w.writeLock(t, lock)

		w.lucyFail(t, "file", "track", "server/mods/sodium.jar")
		w.requireBytes(t, "server/mods/sodium.jar", jar)
		if files := w.readManifest(t).Files; len(files) != 0 {
			t.Errorf("the collision still published a declaration: %+v", files)
		}
	})
}

func TestDisabledPackageLeavesModsFolderAvailable(t *testing.T) {
	w := newWorkspace(t)
	_, nativeLock, _ := w.seedNativeRuntime(t)
	lock := &document.Lock{
		SchemaVersion:       document.LockSchemaVersion,
		ManifestFingerprint: "sha256:" + digestHex([]byte("stale")),
		Server:              nativeLock.Server,
		Packages: []document.Package{{
			Environment: server.EnvironmentServer,
			Ecosystem:   catalog.EcosystemFabric,
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderModrinth,
				ProjectID: "sodium",
				Version:   "0.6.0",
				Filename:  "sodium.jar",
				URL:       "https://example.invalid/sodium.jar",
				Hashes:    map[string]string{"sha256": digestHex([]byte("disabled sodium"))},
			},
			Requirements: []document.RequirementBinding{{
				Reference: "modrinth:sodium",
				Selector:  "stable",
				Enabled:   false,
			}},
		}},
	}
	w.writeLock(t, lock)
	notes := []byte("handwritten note\n")
	w.write(t, "server/mods/notes.txt", notes, 0o644)

	w.lucyOK(t, "file", "track", "server/mods/notes.txt")
	if status := w.statusOf(t, "server/mods/notes.txt"); status.State != "matching" {
		t.Errorf("the disabled package blocked a folder neighbour: %+v", status)
	}
	if declared := w.readManifest(t).Files; len(declared) != 1 || declared[0].Path != "server/mods/notes.txt" {
		t.Errorf("unexpected declaration set: %+v", declared)
	}
}

func TestFileRootRelativeMCDRWrapperLayout(t *testing.T) {
	w := newWorkspace(t)
	_, nativeLock, _ := w.seedNativeRuntime(t)
	lock := &document.Lock{
		SchemaVersion:       document.LockSchemaVersion,
		ManifestFingerprint: "sha256:" + digestHex([]byte("stale")),
		Server:              nativeLock.Server,
		Packages:            []document.Package{},
		MCDR: &document.LockedMCDR{
			Directory: ".",
			Version:   "2.14.0",
		},
	}
	w.writeLock(t, lock)

	notes := []byte("wrapper note\n")
	w.write(t, "wrapper-notes.txt", notes, 0o644)
	w.lucyOK(t, "file", "track", "wrapper-notes.txt")
	if status := w.statusOf(t, "wrapper-notes.txt"); status.State != "matching" {
		t.Errorf("the root-relative wrapper layout rejected a workspace file: %+v", status)
	}
	if declared := w.readManifest(t).Files; len(declared) != 1 || declared[0].Path != "wrapper-notes.txt" {
		t.Errorf("unexpected declaration set: %+v", declared)
	}

	w.write(t, "run-mcdr.sh", []byte("#!/bin/sh\n"), 0o755)
	w.lucyFail(t, "file", "track", "run-mcdr.sh")
	w.requireBytes(t, "run-mcdr.sh", []byte("#!/bin/sh\n"))
	if files := w.readManifest(t).Files; len(files) != 1 {
		t.Errorf("the wrapper collision changed the declaration set: %+v", files)
	}
}
