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

	if baseline.Server().ModLoader() != types.EcoUnspecified {
		t.Fatalf(
			"expected empty dir baseline to look vanilla/none, got %s",
			baseline.Server().ModLoader(),
		)
	}

	targetDir := t.TempDir()
	copyProbeFixture(
		t,
		fixture,
		filepath.Join(targetDir, "fabric-server-launch.jar"),
	)

	observed := NewAt(targetDir)

	if observed.Server().ModLoader() != types.EcoFabric {
		t.Fatalf(
			"expected fabric runtime from target dir, got %s",
			observed.Server().ModLoader(),
		)
	}
	if len(observed.ModPath()) == 0 || observed.ModPath()[0] != filepath.Join(
		targetDir,
		"mods",
	) {
		t.Fatalf(
			"expected absolute fabric mod path from target dir topology, got %v",
			observed.ModPath(),
		)
	}

	cachedAgain := New()

	if cachedAgain.Server().ModLoader() != types.EcoUnspecified {
		t.Fatalf(
			"expected global cache to remain on cache dir, got %s",
			cachedAgain.Server().ModLoader(),
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
	if before.Server().ModLoader() != types.EcoUnspecified {
		t.Fatalf(
			"expected empty dir baseline to look vanilla/none, got %s",
			before.Server().ModLoader(),
		)
	}

	copyProbeFixture(
		t,
		fixture,
		filepath.Join(workDir, "fabric-server-launch.jar"),
	)

	refreshed := Refresh(workDir)
	if refreshed.Server() == nil {
		t.Fatal("expected refreshed runtime info")
	}
	if refreshed.Server().ModLoader() != types.EcoFabric {
		t.Fatalf(
			"expected refresh to rebuild fabric runtime, got %s",
			refreshed.Server().ModLoader(),
		)
	}

	cached := New()
	if cached.Server() == nil {
		t.Fatal("expected cached runtime after refresh")
	}
	if cached.Server().ModLoader() != types.EcoFabric {
		t.Fatalf(
			"expected current-dir cache to be refreshed to fabric, got %s",
			cached.Server().ModLoader(),
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
