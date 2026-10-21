//go:build e2e

package install

import (
	"bytes"
	"context"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/mclucy/lucy/e2e/internal/testkit"
	"gopkg.in/yaml.v3"
)

// TestInitInstall chains initialization and installation: each generated
// sandbox is initialized in place, then the manifest and lock that init wrote
// drive a locked install in an empty workspace. This proves that the lock init
// produces is sufficient for install to replay without resolving again.
func TestInitInstall(t *testing.T) {
	binary := testkit.Lucy(t)
	for _, id := range slices.Sorted(maps.Keys(initInstallScenarios)) {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			executeInitInstall(t, binary, id)
		})
	}
}

func executeInitInstall(t *testing.T, binary, id string) {
	t.Helper()
	scenario := initInstallScenarios[id]
	runner := commandRunner{binary: binary, home: filepath.Join(t.TempDir(), "home")}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()

	source := testkit.CloneSandbox(t, id)
	if _, err := runner.execute(ctx, source, "init", "--no-interactive"); err != nil {
		t.Fatalf("initialize sandbox: %v", err)
	}
	manifestBytes, err := os.ReadFile(filepath.Join(source, "lucy.yaml"))
	if err != nil {
		t.Fatalf("read initialized manifest: %v", err)
	}
	lockBytes, err := os.ReadFile(filepath.Join(source, "lucy-lock.yaml"))
	if err != nil {
		t.Fatalf("read initialized lock: %v", err)
	}
	if _, err := os.Stat(filepath.Join(source, ".lucy", "install.yaml")); !os.IsNotExist(err) {
		t.Fatalf("init staged an installation receipt: %v", err)
	}
	var declared manifestIntent
	if err := yaml.Unmarshal(manifestBytes, &declared); err != nil {
		t.Fatalf("decode initialized manifest: %v", err)
	}
	initialized, err := readLock(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := scenario.verifyInitialized(declared, initialized); err != nil {
		t.Fatalf("initialized workspace: %v", err)
	}

	root := filepath.Join(t.TempDir(), "workspace")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"lucy.yaml":      manifestBytes,
		"lucy-lock.yaml": lockBytes,
	} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := runner.execute(ctx, root, "install", "--locked"); err != nil {
		t.Fatalf("install from initialized lock: %v", err)
	}

	installedManifest, err := os.ReadFile(filepath.Join(root, "lucy.yaml"))
	if err != nil {
		t.Fatalf("read installed manifest: %v", err)
	}
	if !bytes.Equal(installedManifest, manifestBytes) {
		t.Error("install rewrote the manifest that init generated")
	}
	installedLock, err := os.ReadFile(filepath.Join(root, "lucy-lock.yaml"))
	if err != nil {
		t.Fatalf("read installed lock: %v", err)
	}
	if !bytes.Equal(installedLock, lockBytes) {
		t.Error("install resolved again instead of replaying the initialized lock")
	}
	locked, err := readLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := scenario.verifyInstalled(root, locked); err != nil {
		t.Fatalf("installed runtime: %v", err)
	}
}
