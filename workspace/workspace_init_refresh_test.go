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
	if baseline.Runtime == nil {
		t.Fatal("expected baseline runtime info")
	}
	if baseline.DerivedModLoader() != types.EcoBare {
		t.Fatalf(
			"expected empty dir baseline to look vanilla/none, got %s",
			baseline.DerivedModLoader(),
		)
	}

	targetDir := t.TempDir()
	copyProbeFixture(
		t,
		fixture,
		filepath.Join(targetDir, "fabric-server-launch.jar"),
	)

	observed := NewAt(targetDir)
	if observed.Runtime == nil {
		t.Fatal("expected observed runtime info")
	}
	if observed.DerivedModLoader() != types.EcoFabric {
		t.Fatalf(
			"expected fabric runtime from target dir, got %s",
			observed.DerivedModLoader(),
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
	if cachedAgain.Runtime == nil {
		t.Fatal("expected cached runtime info")
	}
	if cachedAgain.DerivedModLoader() != types.EcoBare {
		t.Fatalf(
			"expected global cache to remain on cache dir, got %s",
			cachedAgain.DerivedModLoader(),
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
	if before.Runtime == nil {
		t.Fatal("expected pre-refresh runtime info")
	}
	if before.DerivedModLoader() != types.EcoBare {
		t.Fatalf(
			"expected empty dir baseline to look vanilla/none, got %s",
			before.DerivedModLoader(),
		)
	}

	copyProbeFixture(
		t,
		fixture,
		filepath.Join(workDir, "fabric-server-launch.jar"),
	)

	refreshed := Refresh(workDir)
	if refreshed.Runtime == nil {
		t.Fatal("expected refreshed runtime info")
	}
	if refreshed.DerivedModLoader() != types.EcoFabric {
		t.Fatalf(
			"expected refresh to rebuild fabric runtime, got %s",
			refreshed.DerivedModLoader(),
		)
	}

	cached := New()
	if cached.Runtime == nil {
		t.Fatal("expected cached runtime after refresh")
	}
	if cached.DerivedModLoader() != types.EcoFabric {
		t.Fatalf(
			"expected current-dir cache to be refreshed to fabric, got %s",
			cached.DerivedModLoader(),
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
