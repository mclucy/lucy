package install

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestInstallNeoForgeMod_InstallsToModPath(t *testing.T) {
	modDir := t.TempDir()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("mod"))
	}))
	t.Cleanup(server.Close)

	restore := withMockServerInfo(types.ServerInfo{
		ModPath: []string{modDir},
		Executable: &types.ExecutableInfo{
			ModLoader: types.Neoforge,
		},
	})
	t.Cleanup(restore)

	pkg := types.Package{
		Id: types.PackageId{
			Platform: types.Neoforge,
			Name:     "some-mod",
			Version:  "1.0.0",
		},
		Remote: &types.PackageRemote{
			FileUrl: server.URL + "/some-mod.jar",
		},
	}

	err := installNeoForgeMod(pkg)
	if err != nil {
		t.Fatalf("installNeoForgeMod() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(modDir, "some-mod.jar")); err != nil {
		t.Fatalf("expected mod file in mod path: %v", err)
	}
}

func TestEnsurePlatformMatch_NeoForge(t *testing.T) {
	restore := withMockServerInfo(types.ServerInfo{
		Executable: &types.ExecutableInfo{ModLoader: types.Neoforge},
	})
	t.Cleanup(restore)

	if err := ensurePlatformMatch(types.Neoforge); err != nil {
		t.Fatalf("ensurePlatformMatch(neoforge) error = %v", err)
	}
}
