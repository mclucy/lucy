package install

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	serverpkg "github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
)

func testWorkspace(t *testing.T) (string, *manifest.Document, *lockfile.Document) {
	t.Helper()
	root := t.TempDir()
	d := &manifest.Document{
		SchemaVersion: manifest.SchemaVersion,
		Server: manifest.Server{
			Minecraft: versionpkg.Version("1.21.4"),
			Directory: ".",
			Core: manifest.Core{
				Product:        serverpkg.ProductFabric,
				ProductVersion: versionpkg.Version("0.16.9"),
			},
			Packages: map[catalog.Ecosystem]map[string]manifest.Requirement{},
		},
	}
	if err := manifest.Write(root, d); err != nil {
		t.Fatalf("manifest.Write() error = %v", err)
	}
	fingerprint, err := manifest.Fingerprint(d)
	if err != nil {
		t.Fatalf("manifest.Fingerprint() error = %v", err)
	}
	lock := &lockfile.Document{
		SchemaVersion:       lockfile.SchemaVersion,
		ManifestFingerprint: fingerprint,
		Server: lockfile.Server{
			Directory:   ".",
			Minecraft:   versionpkg.Version("1.21.4"),
			Product:     serverpkg.ProductFabric,
			CoreVersion: versionpkg.Version("0.16.9"),
			Artifact: catalog.Artifact{
				Version:  versionpkg.Version("0.16.9"),
				Filename: "fabric-loader-0.16.9.jar",
			},
			Java: lockfile.Java{Advisory: 21},
		},
		Packages: []lockfile.Package{},
	}
	if err := lockfile.Write(root, lock); err != nil {
		t.Fatalf("lockfile.Write() error = %v", err)
	}
	return root, d, lock
}

func TestPlanInstallReplaysMatchingLock(t *testing.T) {
	root, d, lock := testWorkspace(t)
	plan, err := PlanInstall(context.Background(), root, d, lock, Options{})
	if err != nil {
		t.Fatalf("PlanInstall() error = %v", err)
	}
	if plan.Result.Mode != ModeReplay {
		t.Fatalf("PlanInstall() mode = %q, want %q", plan.Result.Mode, ModeReplay)
	}
	if plan.Resolved != lock {
		t.Fatal("PlanInstall() did not replay the matching lock")
	}
	if plan.PublishLock {
		t.Fatal("PlanInstall() planned a lock publication for a replay")
	}
	if plan.BaseManifestFingerprint == "" || plan.BaseLockFingerprint == "" {
		t.Fatal("PlanInstall() did not record both base fingerprints")
	}
}

func TestPlanInstallRejectsExpectedManifestMismatch(t *testing.T) {
	root, d, lock := testWorkspace(t)
	_, err := PlanInstall(
		context.Background(),
		root,
		d,
		lock,
		Options{ExpectedManifest: "sha256:not-the-manifest"},
	)
	if err == nil || !strings.Contains(err.Error(), "manifest changed") {
		t.Fatalf("PlanInstall() error = %v, want a manifest-change error", err)
	}
}

func TestPlanInstallRejectsExpectedLockMismatch(t *testing.T) {
	root, d, lock := testWorkspace(t)
	_, err := PlanInstall(
		context.Background(),
		root,
		d,
		lock,
		Options{ExpectedLock: "sha256:not-the-lock"},
	)
	if err == nil || !strings.Contains(err.Error(), "lock changed") {
		t.Fatalf("PlanInstall() error = %v, want a lock-change error", err)
	}
}

func TestPlanInstallAcceptsExpectedFingerprints(t *testing.T) {
	root, d, lock := testWorkspace(t)
	manifestFingerprint, err := manifest.Fingerprint(d)
	if err != nil {
		t.Fatalf("manifest.Fingerprint() error = %v", err)
	}
	lockFingerprint, err := lockfile.Fingerprint(lock)
	if err != nil {
		t.Fatalf("lockfile.Fingerprint() error = %v", err)
	}
	if _, err := PlanInstall(
		context.Background(),
		root,
		d,
		lock,
		Options{
			ExpectedManifest: manifestFingerprint,
			ExpectedLock:     lockFingerprint,
		},
	); err != nil {
		t.Fatalf("PlanInstall() error = %v", err)
	}
}

// A plan answers the workspace, not the documents the caller passed: planning
// for a publication keeps the on-disk state as its baseline, so ApplyPlan does
// not mistake the intended edit for drift.
func TestPlanInstallFingerprintsWorkspaceState(t *testing.T) {
	t.Run("manifest edited in memory", func(t *testing.T) {
		root, d, lock := testWorkspace(t)
		onDisk, err := manifest.Read(root)
		if err != nil {
			t.Fatalf("manifest.Read() error = %v", err)
		}
		baseFingerprint, err := manifest.Fingerprint(onDisk)
		if err != nil {
			t.Fatalf("manifest.Fingerprint() error = %v", err)
		}
		d.Server.Java = &manifest.Java{Version: 17}
		plan, err := PlanInstall(
			context.Background(),
			root,
			d,
			lock,
			Options{PublishManifest: true},
		)
		if err != nil {
			t.Fatalf("PlanInstall() error = %v", err)
		}
		if plan.BaseManifestFingerprint != baseFingerprint {
			t.Fatalf(
				"PlanInstall() base manifest fingerprint = %q, want on-disk %q",
				plan.BaseManifestFingerprint,
				baseFingerprint,
			)
		}
		if err := checkPlannedState(root, plan); err != nil {
			t.Fatalf("checkPlannedState() error = %v, want a plan the workspace satisfies", err)
		}
	})

	t.Run("lock differs from workspace", func(t *testing.T) {
		root, d, lock := testWorkspace(t)
		onDisk, err := lockfile.Read(root)
		if err != nil {
			t.Fatalf("lockfile.Read() error = %v", err)
		}
		baseFingerprint, err := lockfile.Fingerprint(onDisk)
		if err != nil {
			t.Fatalf("lockfile.Fingerprint() error = %v", err)
		}
		stale := *lock
		stale.Files = []manifest.TrackedFile{{Path: "server.properties"}}
		plan, err := PlanInstall(context.Background(), root, d, &stale, Options{})
		if err != nil {
			t.Fatalf("PlanInstall() error = %v", err)
		}
		if plan.BaseLockFingerprint != baseFingerprint {
			t.Fatalf(
				"PlanInstall() base lock fingerprint = %q, want on-disk %q",
				plan.BaseLockFingerprint,
				baseFingerprint,
			)
		}
		if err := checkPlannedState(root, plan); err != nil {
			t.Fatalf("checkPlannedState() error = %v, want a plan the workspace satisfies", err)
		}
	})
}

func TestResolveLockRejectsForceWithLocked(t *testing.T) {
	root, d, lock := testWorkspace(t)
	_, err := resolveLock(
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

func TestCheckPlannedStateRejectsChanges(t *testing.T) {
	t.Run("unchanged", func(t *testing.T) {
		root, d, lock := testWorkspace(t)
		plan, err := PlanInstall(context.Background(), root, d, lock, Options{})
		if err != nil {
			t.Fatalf("PlanInstall() error = %v", err)
		}
		if err := checkPlannedState(root, plan); err != nil {
			t.Fatalf("checkPlannedState() error = %v", err)
		}
	})

	t.Run("manifest changed", func(t *testing.T) {
		root, d, lock := testWorkspace(t)
		plan, err := PlanInstall(context.Background(), root, d, lock, Options{})
		if err != nil {
			t.Fatalf("PlanInstall() error = %v", err)
		}
		d.Server.Core.ProductVersion = versionpkg.Version("0.16.10")
		if err := manifest.Write(root, d); err != nil {
			t.Fatalf("manifest.Write() error = %v", err)
		}
		if err := checkPlannedState(root, plan); err == nil {
			t.Fatal("checkPlannedState() error = nil, want a manifest-change error")
		}
	})

	t.Run("manifest removed", func(t *testing.T) {
		root, d, lock := testWorkspace(t)
		plan, err := PlanInstall(context.Background(), root, d, lock, Options{})
		if err != nil {
			t.Fatalf("PlanInstall() error = %v", err)
		}
		if err := os.Remove(filepath.Join(root, manifest.Filename)); err != nil {
			t.Fatalf("remove manifest: %v", err)
		}
		if err := checkPlannedState(root, plan); err == nil {
			t.Fatal("checkPlannedState() error = nil, want a manifest-disappeared error")
		}
	})

	t.Run("lock changed", func(t *testing.T) {
		root, d, lock := testWorkspace(t)
		plan, err := PlanInstall(context.Background(), root, d, lock, Options{})
		if err != nil {
			t.Fatalf("PlanInstall() error = %v", err)
		}
		lock.ManifestFingerprint = "sha256:different"
		if err := lockfile.Write(root, lock); err != nil {
			t.Fatalf("lockfile.Write() error = %v", err)
		}
		if err := checkPlannedState(root, plan); err == nil {
			t.Fatal("checkPlannedState() error = nil, want a lock-change error")
		}
	})

	t.Run("manifest appeared", func(t *testing.T) {
		root, _, _ := testWorkspace(t)
		plan := &Plan{
			Resolved:            &lockfile.Document{},
			BaseLockFingerprint: "sha256:base",
		}
		if err := checkPlannedState(root, plan); err == nil {
			t.Fatal("checkPlannedState() error = nil, want a manifest-appeared error")
		}
	})
}
