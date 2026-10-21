//go:build e2e

// Package init verifies that Lucy initializes manifests from real modpack
// workspaces.
package init

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mclucy/lucy/e2e/internal/testkit"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"gopkg.in/yaml.v3"
)


// TestInitialize runs lucy init against each modpack sandbox and checks the
// manifest and lock that the CLI writes for a real, decompressed server.
func TestInitialize(t *testing.T) {
	binary := testkit.Lucy(t)
	for _, id := range slices.Sorted(maps.Keys(initializeScenarios)) {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			want := initializeScenarios[id]
			if want.blocked != "" {
				t.Skipf("sandbox %s cannot initialize yet: %s", id, want.blocked)
			}
			root := testkit.CloneSandbox(t, id)
			// A local sandbox may carry intent from an earlier run; init must
			// start from the server workspace alone.
			for _, name := range []string{manifest.Filename, lockfile.Filename, ".lucy"} {
				if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
					t.Fatalf("clear generated %s from sandbox: %v", name, err)
				}
			}
			cmd := exec.Command(binary, "init", "--no-interactive", "--no-style")
			cmd.Dir = root
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			stdout, err := cmd.Output()
			if err != nil {
				t.Fatalf(
					"lucy init: %v; stdout: %s; stderr: %s",
					err, stdout, stderr.String(),
				)
			}
			data, err := os.ReadFile(filepath.Join(root, "lucy.yaml"))
			if err != nil {
				t.Fatalf("read generated manifest: %v", err)
			}
			var document manifestDocument
			if err := yaml.Unmarshal(data, &document); err != nil {
				t.Fatalf("decode generated manifest: %v", err)
			}
			server := document.Server
			if server.Minecraft != want.minecraft ||
				server.Core.Product != want.product ||
				server.Core.ProductVersion != want.coreVersion {
				t.Errorf(
					"manifest identity: got %s/%s@%s, want %s/%s@%s",
					server.Minecraft, server.Core.Product, server.Core.ProductVersion,
					want.minecraft, want.product, want.coreVersion,
				)
			}
			total := 0
			for _, packages := range server.Packages {
				total += len(packages)
			}
			if total < want.minPackages {
				t.Errorf("manifest packages: got %d, want at least %d", total, want.minPackages)
			}
			for loader, entries := range want.packages {
				for reference, version := range entries {
					if got := server.Packages[loader][reference]; got != version {
						t.Errorf(
							"manifest package %s [%s]: got %q, want %q",
							reference, loader, got, version,
						)
					}
				}
			}
			for loader, references := range want.absent {
				for _, reference := range references {
					if _, ok := server.Packages[loader][reference]; ok {
						t.Errorf("manifest lists client-only package %s [%s]", reference, loader)
					}
				}
			}
			lockData, err := os.ReadFile(filepath.Join(root, "lucy-lock.yaml"))
			if err != nil {
				t.Fatalf("read generated lock: %v", err)
			}
			locked, err := lockfile.Decode(bytes.NewReader(lockData))
			if err != nil {
				t.Fatalf("decode generated lock: %v", err)
			}
			if locked.Server.Minecraft.String() != want.minecraft ||
				locked.Server.Product.String() != want.product {
				t.Errorf(
					"lock identity: got %s/%s, want %s/%s",
					locked.Server.Minecraft, locked.Server.Product,
					want.minecraft, want.product,
				)
			}
			if len(locked.Packages) < want.minPackages {
				t.Errorf(
					"lock packages: got %d, want at least %d",
					len(locked.Packages), want.minPackages,
				)
			}
			if _, err := os.Stat(filepath.Join(root, ".lucy", "install.yaml")); !os.IsNotExist(err) {
				t.Errorf("init staged an installation receipt: %v", err)
			}
		})
	}
}
