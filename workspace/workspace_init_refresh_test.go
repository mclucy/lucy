package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestWorkspaceAtTargetsWorkDirWithoutPoisoningGlobalCache(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	fixture := filepath.Join(
		originalWD,
		"internal",
		"detector",
		"testdata",
		"fabric",
		"fabric-server-launch.jar",
	)
	t.Cleanup(
		func() {
			_ = os.Chdir(originalWD)
			Invalidate()
		},
	)

	cacheDir := t.TempDir()
	if err := os.Chdir(cacheDir); err != nil {
		t.Fatalf("chdir cache dir: %v", err)
	}
	Invalidate()
	baseline := New()
	if baseline.Server == nil {
		t.Fatal("expected baseline runtime info")
	}
	if baseline.Server.DerivedModLoader() != types.EcoUnspecified {
		t.Fatalf(
			"expected empty dir baseline to look vanilla/none, got %s",
			baseline.Server.DerivedModLoader(),
		)
	}

	targetDir := t.TempDir()
	copyProbeFixture(
		t,
		fixture,
		filepath.Join(targetDir, "fabric-server-launch.jar"),
	)

	observed := NewAt(targetDir)
	if observed.Server == nil {
		t.Fatal("expected observed runtime info")
	}
	if observed.Server.DerivedModLoader() != types.EcoFabric {
		t.Fatalf(
			"expected fabric runtime from target dir, got %s",
			observed.Server.DerivedModLoader(),
		)
	}
	if len(observed.ModPath) == 0 || observed.ModPath[0] != filepath.Join(
		targetDir,
		"mods",
	) {
		t.Fatalf(
			"expected absolute fabric mod path from target dir topology, got %v",
			observed.ModPath,
		)
	}

	cachedAgain := New()
	if cachedAgain.Server == nil {
		t.Fatal("expected cached runtime info")
	}
	if cachedAgain.Server.DerivedModLoader() != types.EcoUnspecified {
		t.Fatalf(
			"expected global cache to remain on cache dir, got %s",
			cachedAgain.Server.DerivedModLoader(),
		)
	}
}

func TestRefreshRebuildsCurrentDirCache(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	fixture := filepath.Join(
		originalWD,
		"internal",
		"detector",
		"testdata",
		"fabric",
		"fabric-server-launch.jar",
	)
	t.Cleanup(
		func() {
			_ = os.Chdir(originalWD)
			Invalidate()
		},
	)

	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir work dir: %v", err)
	}
	Invalidate()
	before := New()
	if before.Server == nil {
		t.Fatal("expected pre-refresh runtime info")
	}
	if before.Server.DerivedModLoader() != types.EcoUnspecified {
		t.Fatalf(
			"expected empty dir baseline to look vanilla/none, got %s",
			before.Server.DerivedModLoader(),
		)
	}

	copyProbeFixture(
		t,
		fixture,
		filepath.Join(workDir, "fabric-server-launch.jar"),
	)

	refreshed := Refresh(workDir)
	if refreshed.Server == nil {
		t.Fatal("expected refreshed runtime info")
	}
	if refreshed.Server.DerivedModLoader() != types.EcoFabric {
		t.Fatalf(
			"expected refresh to rebuild fabric runtime, got %s",
			refreshed.Server.DerivedModLoader(),
		)
	}

	cached := New()
	if cached.Server == nil {
		t.Fatal("expected cached runtime after refresh")
	}
	if cached.Server.DerivedModLoader() != types.EcoFabric {
		t.Fatalf(
			"expected current-dir cache to be refreshed to fabric, got %s",
			cached.Server.DerivedModLoader(),
		)
	}
}

func copyProbeFixture(t *testing.T, src, dst string) {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read fixture %s: %v", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", dst, err)
	}
}
