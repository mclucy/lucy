//go:build e2e

package install

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
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
	if err := verifyInitializedReceipt(source); err != nil {
		t.Fatalf("initialized receipt: %v", err)
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

// verifyInitializedReceipt checks that initialization recorded the packages it
// identified. A workspace without identified packages keeps no receipt.
func verifyInitializedReceipt(root string) error {
	locked, err := readLock(root)
	if err != nil {
		return err
	}
	path := filepath.Join(root, ".lucy", "install.yaml")
	raw, err := os.ReadFile(path)
	if len(locked.Packages) == 0 {
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		return fmt.Errorf("init recorded a receipt for a workspace without identified packages")
	}
	if err != nil {
		return fmt.Errorf("read initialized receipt: %w", err)
	}
	var receipt struct {
		Entries []struct {
			Path   string `yaml:"path"`
			SHA256 string `yaml:"sha256"`
		} `yaml:"entries"`
	}
	if err := yaml.Unmarshal(raw, &receipt); err != nil {
		return fmt.Errorf("decode initialized receipt: %w", err)
	}
	recorded := make(map[string]bool, len(receipt.Entries))
	for _, entry := range receipt.Entries {
		recorded[filepath.Base(filepath.FromSlash(entry.Path))] = true
		actual, err := fileSHA256(filepath.Join(root, filepath.FromSlash(entry.Path)))
		if err != nil {
			return fmt.Errorf("recorded file %s: %w", entry.Path, err)
		}
		if actual != entry.SHA256 {
			return fmt.Errorf("recorded digest for %s does not match the file", entry.Path)
		}
	}
	for _, pkg := range locked.Packages {
		if !recorded[pkg.Filename] {
			return fmt.Errorf("receipt records no entry for locked package %s", pkg.Filename)
		}
	}
	return nil
}

// adoptionScenario describes a generated server directory that is already
// installed by its upstream installer. Initialization adopts it and touches no
// installation output; the installation that follows must replace the files
// only it can own instead of refusing the adopted runtime.
type adoptionScenario struct {
	minecraft   string
	product     string
	coreVersion string
	// packages requires the lock to keep the packages the directory ships.
	packages int
	// unchanged lists sandbox files whose bytes must survive both commands.
	unchanged []string
}

var adoptionScenarios = map[string]adoptionScenario{
	"fabric-installed-1214": {
		minecraft: "1.21.4", product: "fabric", coreVersion: "0.16.9",
	},
	"fabric-installed-262": {
		minecraft: "26.2", product: "fabric", coreVersion: "0.19.3",
		packages:  1,
		unchanged: []string{"mods/fabric-api-0.158.0+26.2.jar"},
	},
}

// TestAdoptInstall installs in place after initialization. The adopted
// directory carries the upstream installer's launcher, which is not
// byte-identical to the one Lucy generates, and packages that no receipt
// records. Both must be recognized as part of the adopted runtime.
func TestAdoptInstall(t *testing.T) {
	binary := testkit.Lucy(t)
	for _, id := range slices.Sorted(maps.Keys(adoptionScenarios)) {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			scenario := adoptionScenarios[id]
			root := testkit.CloneSandbox(t, id)
			// A local sandbox may carry intent from an earlier run; adoption
			// starts from the files the upstream installer produced.
			for _, name := range []string{"lucy.yaml", "lucy-lock.yaml", ".lucy"} {
				if err := os.RemoveAll(filepath.Join(root, name)); err != nil {
					t.Fatalf("reset sandbox %s: %v", name, err)
				}
			}
			launcher := filepath.Join(root, "fabric-server-launch.jar")
			appendAdoptedComment(t, launcher)
			runner := commandRunner{binary: binary, home: filepath.Join(t.TempDir(), "home")}
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
			defer cancel()

			if _, err := runner.execute(ctx, root, "init", "--no-interactive"); err != nil {
				t.Fatalf("initialize sandbox: %v", err)
			}
			unchanged := make(map[string]string, len(scenario.unchanged))
			for _, relative := range scenario.unchanged {
				digest, err := fileSHA256(filepath.Join(root, filepath.FromSlash(relative)))
				if err != nil {
					t.Fatalf("read sandbox file %s: %v", relative, err)
				}
				unchanged[relative] = digest
			}
			if _, err := runner.execute(ctx, root, "install", "--no-interactive"); err != nil {
				t.Fatalf("install adopted workspace: %v", err)
			}

			if _, err := os.Stat(filepath.Join(root, ".lucy", "install.yaml")); err != nil {
				t.Fatalf("adopted installation wrote no receipt: %v", err)
			}
			locked, err := readLock(root)
			if err != nil {
				t.Fatal(err)
			}
			if locked.Server.Product != scenario.product ||
				locked.Server.Minecraft != scenario.minecraft ||
				locked.Server.CoreVersion != scenario.coreVersion {
				t.Fatalf(
					"installed identity: got %s/%s/%s, want %s/%s/%s",
					locked.Server.Product, locked.Server.Minecraft, locked.Server.CoreVersion,
					scenario.product, scenario.minecraft, scenario.coreVersion,
				)
			}
			if len(locked.Packages) < scenario.packages {
				t.Fatalf(
					"installed packages: got %d, want at least %d",
					len(locked.Packages), scenario.packages,
				)
			}
			live := filepath.Join(root, filepath.FromSlash(locked.Server.Directory))
			if err := verifyBootstrapInputs(live, locked.Server.Inputs, nil); err != nil {
				t.Fatalf("bootstrap inputs: %v", err)
			}
			for _, relative := range []string{
				"run.sh",
				"run.bat",
				"fabric-server-launch.jar",
				"fabric-server-launcher.properties",
			} {
				if _, err := os.Stat(filepath.Join(live, relative)); err != nil {
					t.Fatalf("managed launcher %s: %v", relative, err)
				}
			}
			generatedShim, err := fileSHA256(filepath.Join(live, "fabric-server-launch.jar"))
			if err != nil {
				t.Fatal(err)
			}
			launcherBytes, err := os.ReadFile(filepath.Join(live, "fabric-server-launch.jar"))
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(launcherBytes, adoptedLauncherComment) {
				t.Error("install kept the adopted launcher instead of generating its own")
			}
			for relative, want := range unchanged {
				actual, err := fileSHA256(filepath.Join(root, filepath.FromSlash(relative)))
				if err != nil {
					t.Fatalf("read adopted file %s: %v", relative, err)
				}
				if actual != want {
					t.Errorf("install rewrote adopted file %s", relative)
				}
			}

			// The receipt now owns the runtime, so a second installation is a
			// no-op rather than another adoption.
			if _, err := runner.execute(ctx, root, "install", "--no-interactive"); err != nil {
				t.Fatalf("reinstall adopted workspace: %v", err)
			}
			again, err := fileSHA256(filepath.Join(live, "fabric-server-launch.jar"))
			if err != nil {
				t.Fatal(err)
			}
			if again != generatedShim {
				t.Error("reinstall regenerated the launcher it already owns")
			}
		})
	}
}

var adoptedLauncherComment = []byte("\nwritten by the upstream installer\n")

// appendAdoptedComment makes the adopted launcher differ from every launcher
// Lucy generates. The file is detached from the sandbox fixture first, because
// cloning hardlinks it.
func appendAdoptedComment(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read adopted launcher: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat adopted launcher: %v", err)
	}
	end := bytes.LastIndex(data, []byte("PK\x05\x06"))
	if end < 0 || end+22 > len(data) {
		t.Fatal("adopted launcher has no ZIP end record")
	}
	commentLength := int(binary.LittleEndian.Uint16(data[end+20 : end+22]))
	if end+22+commentLength != len(data) {
		t.Fatal("adopted launcher has an unexpected ZIP layout")
	}
	binary.LittleEndian.PutUint16(
		data[end+20:end+22],
		uint16(commentLength+len(adoptedLauncherComment)),
	)
	data = append(data, adoptedLauncherComment...)
	if err := os.Remove(path); err != nil {
		t.Fatalf("detach adopted launcher: %v", err)
	}
	if err := os.WriteFile(path, data, info.Mode().Perm()); err != nil {
		t.Fatalf("write adopted launcher: %v", err)
	}
}
