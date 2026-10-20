//go:build e2e

package status

import (
	"bytes"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mclucy/lucy/e2e/internal/testkit"
	"gopkg.in/yaml.v3"
)

type manifestDocument struct {
	Server struct {
		Minecraft string `yaml:"minecraft"`
		Core      struct {
			Product        string `yaml:"product"`
			ProductVersion string `yaml:"product_version"`
		} `yaml:"core"`
		Packages map[string]map[string]string `yaml:"packages"`
	} `yaml:"server"`
}

type initializeExpectation struct {
	minecraft   string
	product     string
	coreVersion string
	minPackages int
	// packages maps a loader to the references that the initializer must write.
	packages map[string]map[string]string
	// absent lists references that the initializer must omit because their jars
	// are client-only.
	absent map[string][]string
}

var initializeScenarios = map[string]initializeExpectation{
	"cobblemon-1211": {
		minecraft: "1.21.1", product: "fabric", coreVersion: "0.19.5",
		minPackages: 30,
		packages: map[string]map[string]string{
			"fabric": {
				"modrinth:advanced-loot-info": "1.21.1-2.4.0",
				"modrinth:cobblemon":          "1.8.2",
				"modrinth:fabric-api":         "0.116.17+1.21.1",
			},
		},
		absent: map[string][]string{
			"fabric": {"modrinth:enchdesc", "modrinth:tipsmod"},
		},
	},
	"create-plus-1211": {
		minecraft: "1.21.1", product: "neoforge", coreVersion: "21.1.233",
		minPackages: 100,
		packages: map[string]map[string]string{
			"forge":    {"modrinth:txnilib": "neoforge-1.21.1-1.0.24"},
			"neoforge": {"modrinth:aileron": "1.21.1-neoforge-1.1.4"},
		},
	},
}

// TestInitialize runs lucy init against each modpack sandbox and checks the
// manifest that the CLI writes for a real, decompressed server.
func TestInitialize(t *testing.T) {
	binary := testkit.Lucy(t)
	for _, id := range slices.Sorted(maps.Keys(initializeScenarios)) {
		t.Run(id, func(t *testing.T) {
			t.Parallel()
			want := initializeScenarios[id]
			root := testkit.CloneSandbox(t, id)
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
		})
	}
}
