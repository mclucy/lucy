package install

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/workspacewrite"
	serverpkg "github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
)

func testWorkspace(t *testing.T) (string, *document.Manifest, *document.Lock) {
	t.Helper()
	root := t.TempDir()
	d := &document.Manifest{
		SchemaVersion: document.ManifestSchemaVersion,
		Server: document.ManifestServer{
			Minecraft: versionpkg.Version("1.21.4"),
			Directory: ".",
			Core: document.ManifestCore{
				Product:        serverpkg.ProductFabric,
				ProductVersion: versionpkg.Version("0.16.9"),
			},
			Packages: map[catalog.Ecosystem]map[string]document.Requirement{},
		},
	}
	if err := document.ManifestFile(root).Write(d); err != nil {
		t.Fatalf("manifest.Write() error = %v", err)
	}
	fingerprint, err := d.Fingerprint()
	if err != nil {
		t.Fatalf("manifest.Fingerprint() error = %v", err)
	}
	lock := &document.Lock{
		SchemaVersion:       document.LockSchemaVersion,
		ManifestFingerprint: fingerprint,
		Server: document.LockedServer{
			Directory:   ".",
			Minecraft:   versionpkg.Version("1.21.4"),
			Product:     serverpkg.ProductFabric,
			CoreVersion: versionpkg.Version("0.16.9"),
			Artifact: catalog.Artifact{
				Version:  versionpkg.Version("0.16.9"),
				Filename: "fabric-loader-0.16.9.jar",
				Modules: []catalog.DescriptorModule{
					{ID: "fabricloader", Version: versionpkg.Version("0.16.9")},
				},
			},
			Java: document.LockedJava{Advisory: 21},
		},
		Packages: []document.Package{},
	}
	if err := document.LockFile(root).Write(lock); err != nil {
		t.Fatalf("lockfile.Write() error = %v", err)
	}
	return root, d, lock
}

func testInstallService() *Service {
	return &Service{}
}

func TestResolveReplaysUnchangedLock(t *testing.T) {
	root, d, lock := testWorkspace(t)
	holdWorkspaceLock(t, root)
	resolved, result, err := testInstallService().Resolve(
		context.Background(),
		root,
		d,
		lock,
		Options{},
	)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if result.Mode != ModeReplay {
		t.Fatalf("Resolve() mode = %q, want %q", result.Mode, ModeReplay)
	}
	if resolved != lock {
		t.Fatal("Resolve() did not replay the matching lock")
	}
	if result.PublishedLock {
		t.Fatal("Resolve() reported a lock publication for a replay")
	}
}

func TestPreflightRejectsExpectedManifestMismatch(t *testing.T) {
	root, d, lock := testWorkspace(t)
	holdWorkspaceLock(t, root)
	_, _, err := preflightLocked(
		root,
		d,
		lock,
		Options{ExpectedManifest: "sha256:not-the-manifest"},
		true,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "manifest changed") {
		t.Fatalf("preflightLocked() error = %v, want a manifest-change error", err)
	}
}

func TestPreflightRejectsExpectedLockMismatch(t *testing.T) {
	root, d, lock := testWorkspace(t)
	holdWorkspaceLock(t, root)
	_, _, err := preflightLocked(
		root,
		d,
		lock,
		Options{ExpectedLock: "sha256:not-the-lock"},
		true,
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "lock changed") {
		t.Fatalf("preflightLocked() error = %v, want a lock-change error", err)
	}
}

func TestPreflightAcceptsExpectedFingerprints(t *testing.T) {
	root, d, lock := testWorkspace(t)
	holdWorkspaceLock(t, root)
	manifestFingerprint, err := d.Fingerprint()
	if err != nil {
		t.Fatalf("manifest.Fingerprint() error = %v", err)
	}
	lockFingerprint, err := lock.Fingerprint()
	if err != nil {
		t.Fatalf("lockfile.Fingerprint() error = %v", err)
	}
	if _, _, err := preflightLocked(
		root,
		d,
		lock,
		Options{
			ExpectedManifest: manifestFingerprint,
			ExpectedLock:     lockFingerprint,
		},
		true,
		nil,
	); err != nil {
		t.Fatalf("preflightLocked() error = %v", err)
	}
}

// TestPlanFromResolutionFingerprintsWorkspaceState keeps the on-disk baseline
// distinct from intended publication edits, so ApplyPlan does not mistake the
// intended edit for drift.
func TestPlanFromResolutionFingerprintsWorkspaceState(t *testing.T) {
	root, d, lock := testWorkspace(t)
	onDisk, err := document.ManifestFile(root).Read()
	if err != nil {
		t.Fatalf("manifest.Read() error = %v", err)
	}
	baseFingerprint, err := onDisk.Fingerprint()
	if err != nil {
		t.Fatalf("manifest.Fingerprint() error = %v", err)
	}
	d.Server.Java = &document.ManifestJava{Version: 17}
	finalFingerprint, err := d.Fingerprint()
	if err != nil {
		t.Fatalf("manifest.Fingerprint() error = %v", err)
	}
	resolved := *lock
	resolved.ManifestFingerprint = finalFingerprint
	plan, err := PlanFromResolution(d, lock, &resolved, baseFingerprint, true)
	if err != nil {
		t.Fatalf("PlanFromResolution() error = %v", err)
	}
	if plan.BaseManifestFingerprint != baseFingerprint {
		t.Fatalf(
			"plan base manifest fingerprint = %q, want on-disk %q",
			plan.BaseManifestFingerprint,
			baseFingerprint,
		)
	}
	if err := checkPlannedState(root, plan); err != nil {
		t.Fatalf("checkPlannedState() error = %v, want a plan the workspace satisfies", err)
	}
}

func TestResolveLockRejectsForceWithLocked(t *testing.T) {
	root, d, lock := testWorkspace(t)
	_, err := testInstallService().resolveLock(
		context.Background(),
		root,
		d,
		lock,
		&Options{ForceResolve: true, Locked: true},
		nil,
		newResult(),
	)
	if err == nil || !strings.Contains(err.Error(), "cannot force resolution") {
		t.Fatalf("resolveLock() error = %v, want a force/locked conflict", err)
	}
}

func holdWorkspaceLock(t *testing.T, root string) {
	t.Helper()
	writer, err := workspacewrite.Lock(root, nil)
	if err != nil {
		t.Fatalf("workspacewrite.Lock() error = %v", err)
	}
	t.Cleanup(func() {
		if err := writer.Close(); err != nil {
			t.Errorf("writer.Close() error = %v", err)
		}
	})
}

func testPlanFixture(
	t *testing.T,
	root string,
	d *document.Manifest,
	lock *document.Lock,
) *Plan {
	t.Helper()
	manifestFingerprint, _, err := workspaceFingerprints(root)
	if err != nil {
		t.Fatalf("workspaceFingerprints() error = %v", err)
	}
	plan, err := PlanFromResolution(d, lock, lock, manifestFingerprint, false)
	if err != nil {
		t.Fatalf("PlanFromResolution() error = %v", err)
	}
	return plan
}

func TestCheckPlannedStateRejectsChanges(t *testing.T) {
	t.Run("unchanged", func(t *testing.T) {
		root, d, lock := testWorkspace(t)
		plan := testPlanFixture(t, root, d, lock)
		if err := checkPlannedState(root, plan); err != nil {
			t.Fatalf("checkPlannedState() error = %v", err)
		}
	})

	t.Run("manifest changed", func(t *testing.T) {
		root, d, lock := testWorkspace(t)
		plan := testPlanFixture(t, root, d, lock)
		d.Server.Core.ProductVersion = versionpkg.Version("0.16.10")
		if err := document.ManifestFile(root).Write(d); err != nil {
			t.Fatalf("manifest.Write() error = %v", err)
		}
		if err := checkPlannedState(root, plan); err == nil {
			t.Fatal("checkPlannedState() error = nil, want a manifest-change error")
		}
	})

	t.Run("manifest removed", func(t *testing.T) {
		root, d, lock := testWorkspace(t)
		plan := testPlanFixture(t, root, d, lock)
		if err := os.Remove(filepath.Join(root, document.ManifestFilename)); err != nil {
			t.Fatalf("remove manifest: %v", err)
		}
		if err := checkPlannedState(root, plan); err == nil {
			t.Fatal("checkPlannedState() error = nil, want a manifest-disappeared error")
		}
	})

	t.Run("lock changed", func(t *testing.T) {
		root, d, lock := testWorkspace(t)
		plan := testPlanFixture(t, root, d, lock)
		lock.ManifestFingerprint = "sha256:different"
		if err := document.LockFile(root).Write(lock); err != nil {
			t.Fatalf("lockfile.Write() error = %v", err)
		}
		if err := checkPlannedState(root, plan); err == nil {
			t.Fatal("checkPlannedState() error = nil, want a lock-change error")
		}
	})

	t.Run("manifest appeared", func(t *testing.T) {
		root, _, _ := testWorkspace(t)
		plan := &Plan{
			Resolved:            &document.Lock{},
			BaseLockFingerprint: "sha256:base",
		}
		if err := checkPlannedState(root, plan); err == nil {
			t.Fatal("checkPlannedState() error = nil, want a manifest-appeared error")
		}
	})
}
