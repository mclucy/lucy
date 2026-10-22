package workspace

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/catalog"
)

func TestWorkspaceAtTargetsWorkDirWithoutPoisoningGlobalCache(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
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

	if baseline.Runtime().Ecosystem() != catalog.EcosystemUnspecified {
		t.Fatalf(
			"expected empty dir baseline to look vanilla/none, got %s",
			baseline.Runtime().Ecosystem(),
		)
	}

	targetDir := t.TempDir()
	writeTestFabricLauncherJar(
		t,
		filepath.Join(targetDir, "fabric-server-launch.jar"),
	)

	observed := NewAt(targetDir)

	if observed.Runtime().Ecosystem() != catalog.EcosystemFabric {
		t.Fatalf(
			"expected fabric runtime from target dir, got %s",
			observed.Runtime().Ecosystem(),
		)
	}
	if len(observed.PackageDirs()) == 0 || observed.PackageDirs()[0] != filepath.Join(
		targetDir,
		"mods",
	) {
		t.Fatalf(
			"expected absolute fabric package dirs from target dir topology, got %v",
			observed.PackageDirs(),
		)
	}

	cachedAgain := New()

	if cachedAgain.Runtime().Ecosystem() != catalog.EcosystemUnspecified {
		t.Fatalf(
			"expected global cache to remain on cache dir, got %s",
			cachedAgain.Runtime().Ecosystem(),
		)
	}
}

func TestRefreshRebuildsCurrentDirCache(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
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
	if before.Runtime().Ecosystem() != catalog.EcosystemUnspecified {
		t.Fatalf(
			"expected empty dir baseline to look vanilla/none, got %s",
			before.Runtime().Ecosystem(),
		)
	}

	writeTestFabricLauncherJar(
		t,
		filepath.Join(workDir, "fabric-server-launch.jar"),
	)

	refreshed := Refresh(workDir)
	if refreshed.Runtime() == nil {
		t.Fatal("expected refreshed runtime info")
	}
	if refreshed.Runtime().Ecosystem() != catalog.EcosystemFabric {
		t.Fatalf(
			"expected refresh to rebuild fabric runtime, got %s",
			refreshed.Runtime().Ecosystem(),
		)
	}

	cached := New()
	if cached.Runtime() == nil {
		t.Fatal("expected cached runtime after refresh")
	}
	if cached.Runtime().Ecosystem() != catalog.EcosystemFabric {
		t.Fatalf(
			"expected current-dir cache to be refreshed to fabric, got %s",
			cached.Runtime().Ecosystem(),
		)
	}
}

func writeTestFabricLauncherJar(t *testing.T, dst string) {
	t.Helper()
	file, err := os.Create(dst)
	if err != nil {
		t.Fatalf("create test fabric jar: %v", err)
	}
	defer file.Close()

	w := zip.NewWriter(file)
	manifest := "Manifest-Version: 1.0\nMain-Class: net.fabricmc.loader.impl.launch.server.FabricServerLauncher\nClass-Path: libraries/net/fabricmc/intermediary/1.21.4/intermediary-1.21.4.jar libraries/net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar\n"
	properties := "launch.mainClass=net.fabricmc.loader.impl.launch.knot.KnotServer\n"

	mf, err := w.Create("META-INF/MANIFEST.MF")
	if err != nil {
		t.Fatalf("create manifest in zip: %v", err)
	}
	if _, err := mf.Write([]byte(manifest)); err != nil {
		t.Fatalf("write manifest to zip: %v", err)
	}

	prop, err := w.Create("fabric-server-launch.properties")
	if err != nil {
		t.Fatalf("create properties in zip: %v", err)
	}
	if _, err := prop.Write([]byte(properties)); err != nil {
		t.Fatalf("write properties to zip: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
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
