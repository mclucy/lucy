package probe

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestServerInfoAtTargetsWorkDirWithoutPoisoningGlobalCache(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	fixture := filepath.Join(originalWD, "internal", "detector", "testdata", "fabric", "fabric-server-launch.jar")
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
		InvalidateServerInfo()
	})

	cacheDir := t.TempDir()
	if err := os.Chdir(cacheDir); err != nil {
		t.Fatalf("chdir cache dir: %v", err)
	}
	InvalidateServerInfo()
	baseline := ServerInfo()
	if baseline.Runtime == nil {
		t.Fatal("expected baseline runtime info")
	}
	if baseline.Runtime.DerivedModLoader() != types.PlatformNone {
		t.Fatalf("expected empty dir baseline to look vanilla/none, got %s", baseline.Runtime.DerivedModLoader())
	}

	targetDir := t.TempDir()
	copyProbeFixture(
		t,
		fixture,
		filepath.Join(targetDir, "fabric-server-launch.jar"),
	)

	observed := ServerInfoAt(targetDir)
	if observed.Runtime == nil {
		t.Fatal("expected observed runtime info")
	}
	if observed.Runtime.DerivedModLoader() != types.PlatformFabric {
		t.Fatalf("expected fabric runtime from target dir, got %s", observed.Runtime.DerivedModLoader())
	}
	if len(observed.ModPath) == 0 || observed.ModPath[0] != "mods" {
		t.Fatalf("expected fabric mod path candidate from target dir topology, got %v", observed.ModPath)
	}

	cachedAgain := ServerInfo()
	if cachedAgain.Runtime == nil {
		t.Fatal("expected cached runtime info")
	}
	if cachedAgain.Runtime.DerivedModLoader() != types.PlatformNone {
		t.Fatalf("expected global cache to remain on cache dir, got %s", cachedAgain.Runtime.DerivedModLoader())
	}
}

func TestRefreshServerInfoRebuildsCurrentDirCache(t *testing.T) {
	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	fixture := filepath.Join(originalWD, "internal", "detector", "testdata", "fabric", "fabric-server-launch.jar")
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
		InvalidateServerInfo()
	})

	workDir := t.TempDir()
	if err := os.Chdir(workDir); err != nil {
		t.Fatalf("chdir work dir: %v", err)
	}
	InvalidateServerInfo()
	before := ServerInfo()
	if before.Runtime == nil {
		t.Fatal("expected pre-refresh runtime info")
	}
	if before.Runtime.DerivedModLoader() != types.PlatformNone {
		t.Fatalf("expected empty dir baseline to look vanilla/none, got %s", before.Runtime.DerivedModLoader())
	}

	copyProbeFixture(
		t,
		fixture,
		filepath.Join(workDir, "fabric-server-launch.jar"),
	)

	refreshed := RefreshServerInfo(workDir)
	if refreshed.Runtime == nil {
		t.Fatal("expected refreshed runtime info")
	}
	if refreshed.Runtime.DerivedModLoader() != types.PlatformFabric {
		t.Fatalf("expected refresh to rebuild fabric runtime, got %s", refreshed.Runtime.DerivedModLoader())
	}

	cached := ServerInfo()
	if cached.Runtime == nil {
		t.Fatal("expected cached runtime after refresh")
	}
	if cached.Runtime.DerivedModLoader() != types.PlatformFabric {
		t.Fatalf("expected current-dir cache to be refreshed to fabric, got %s", cached.Runtime.DerivedModLoader())
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
