//go:build e2e

package install

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mclucy/lucy/e2e/internal/testkit"
	"gopkg.in/yaml.v3"
)

type installScenario struct {
	// sandbox names the generated server that supplies pinned oracle bytes.
	// Scenarios without one pin their own digests below, so fixtures that are
	// not real servers never need a generated directory or a committed binary.
	sandbox          string
	product          string
	minecraft        string
	coreVersion      string
	provider         string
	projectID        string
	filename         string
	bootstrap        string
	coreOutput       string
	coreFixture      string
	minecraftFixture string
	minecraftDigest  string
	requiredFiles    []string
	requiredInputs   []string
	launchArguments  string
	runtimeArtifacts []runtimeArtifactExpectation
	packages         []packageExpectation
	// packageList names a YAML file under testdata that supplies the expected
	// packages. Use it for translated modpacks whose package set is too large
	// to read in Go.
	packageList string
}

type runtimeArtifactExpectation struct {
	sandboxFile, runtimeFile string
}

type packageExpectation struct {
	Reference string `yaml:"reference"`
	ProjectID string `yaml:"projectID"`
	ReleaseID string `yaml:"releaseID"`
	FileID    string `yaml:"fileID"`
	Filename  string `yaml:"filename"`
	SHA256    string `yaml:"sha256"`
}

var installScenarios = map[string]installScenario{
	"vanilla-1201": {
		sandbox:          "vanilla-1201",
		product:          "minecraft",
		minecraft:        "1.20.1",
		coreVersion:      "1.20.1",
		provider:         "mojang",
		projectID:        "minecraft",
		filename:         "server.jar",
		coreOutput:       "server.jar",
		coreFixture:      "server.jar",
		minecraftFixture: "server.jar",
	},
	"fabric-1214": {
		product:         "fabric",
		minecraft:       "1.21.4",
		coreVersion:     "0.16.9",
		provider:        "fabric",
		projectID:       "fabric-loader",
		filename:        "fabric-loader-0.16.9.jar",
		bootstrap:       "fabric",
		coreOutput:      "libraries/net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar",
		minecraftDigest: "1066970b09e9c671844572291c4a871cc1ac2b85838bf7004fa0e778e10f1358",
		requiredFiles: []string{
			"run.sh",
			"run.bat",
			"fabric-server-launcher.properties",
		},
		packages: []packageExpectation{
			{
				Reference: "modrinth:fabric-api", ProjectID: "P7dR8mSH", ReleaseID: "p96k10UR",
				FileID: "9aQezMA2", Filename: "fabric-api-0.119.4+1.21.4.jar",
				SHA256: "d183bacb845167f09264c2f90322b7ecffe8826debda6f60e597889264bef4af",
			},
			{
				Reference: "modrinth:flan", ProjectID: "Si383TIH", ReleaseID: "9EC25rAV",
				FileID: "HjsqW7BQ", Filename: "flan-1.21.4-1.11.7.b-fabric.jar",
				SHA256: "33c466b7c319f1597798f62d13b619317590bf069c4a2c11715fb19fdd98e7f8",
			},
		},
	},
	"forge-1201": {
		sandbox:         "forge-1201",
		product:         "forge",
		minecraft:       "1.20.1",
		coreVersion:     "47.4.10",
		provider:        "forge",
		projectID:       "forge",
		filename:        "forge-1.20.1-47.4.10-installer.jar",
		bootstrap:       "forge-installer",
		coreFixture:     "forge-1.20.1-47.4.10-installer.jar",
		requiredFiles:   []string{"run.sh", "user_jvm_args.txt"},
		requiredInputs:  []string{"libraries/net/minecraftforge/forge/1.20.1-47.4.10/forge-1.20.1-47.4.10-universal.jar"},
		launchArguments: "libraries/net/minecraftforge/forge/1.20.1-47.4.10/unix_args.txt",
		runtimeArtifacts: []runtimeArtifactExpectation{{
			sandboxFile: "libraries/net/minecraftforge/forge/1.20.1-47.4.10/forge-1.20.1-47.4.10-universal.jar",
			runtimeFile: "libraries/net/minecraftforge/forge/1.20.1-47.4.10/forge-1.20.1-47.4.10-universal.jar",
		}},
	},
	"neoforge-211": {
		sandbox:         "neoforge-211",
		product:         "neoforge",
		minecraft:       "1.21.1",
		coreVersion:     "21.1.248",
		provider:        "neoforge",
		projectID:       "neoforge",
		filename:        "neoforge-21.1.248-installer.jar",
		bootstrap:       "forge-installer",
		coreFixture:     "neoforge-21.1.248-installer.jar",
		requiredFiles:   []string{"run.sh", "user_jvm_args.txt"},
		requiredInputs:  []string{"libraries/net/neoforged/neoforge/21.1.248/neoforge-21.1.248-universal.jar"},
		launchArguments: "libraries/net/neoforged/neoforge/21.1.248/unix_args.txt",
		runtimeArtifacts: []runtimeArtifactExpectation{{
			sandboxFile: "libraries/net/neoforged/neoforge/21.1.248/neoforge-21.1.248-universal.jar",
			runtimeFile: "libraries/net/neoforged/neoforge/21.1.248/neoforge-21.1.248-universal.jar",
		}},
	},
	"cobblemon-1211": {
		sandbox:         "cobblemon-1211",
		product:         "fabric",
		minecraft:       "1.21.1",
		coreVersion:     "0.19.5",
		provider:        "fabric",
		projectID:       "fabric-loader",
		filename:        "fabric-loader-0.19.5.jar",
		bootstrap:       "fabric",
		coreOutput:      "libraries/net/fabricmc/fabric-loader/0.19.5/fabric-loader-0.19.5.jar",
		minecraftDigest: "e3bc55693e93cda0188f2e60aea28113fc647c5e85a15fa3d1b347349231b4bb",
		requiredFiles: []string{
			"run.sh",
			"run.bat",
			"fabric-server-launcher.properties",
		},
		packageList: "cobblemon-1211.packages.yaml",
	},
	"paper-12111": {
		sandbox:       "paper",
		product:       "paper",
		minecraft:     "1.21.11",
		coreVersion:   "130",
		provider:      "papermc",
		projectID:     "paper",
		filename:      "paper-1.21.11-130.jar",
		bootstrap:     "paperclip",
		coreOutput:    "server.jar",
		coreFixture:   "paper-1.21.11-130.jar",
		requiredFiles: []string{"server.jar", "versions/1.21.11/paper-1.21.11.jar"},
		requiredInputs: []string{
			"cache/mojang_1.21.11.jar",
		},
	},
	"purpur-12111": {
		sandbox:       "purpur",
		product:       "purpur",
		minecraft:     "1.21.11",
		coreVersion:   "2568",
		provider:      "purpur",
		projectID:     "purpur",
		filename:      "purpur-1.21.11-2568.jar",
		bootstrap:     "paperclip",
		coreOutput:    "server.jar",
		coreFixture:   "server.jar",
		requiredFiles: []string{"server.jar", "versions/1.21.11/purpur-1.21.11.jar"},
		requiredInputs: []string{
			"cache/mojang_1.21.11.jar",
		},
	},
}

func executeScenario(t *testing.T, binary, id string) {
	t.Helper()
	scenario := installScenarios[id]
	if scenario.packageList != "" {
		path := filepath.Join(testkit.RepoRoot(t), "e2e", "install", "testdata", scenario.packageList)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read package list: %v", err)
		}
		if err := yaml.Unmarshal(data, &scenario.packages); err != nil {
			t.Fatalf("decode package list: %v", err)
		}
	}
	oracle := ""
	if scenario.sandbox != "" {
		oracle = testkit.Sandbox(t, scenario.sandbox)
	}
	intentPath := filepath.Join(testkit.RepoRoot(t), "e2e", "install", "testdata", id+".yaml")
	intent, err := os.ReadFile(intentPath)
	if err != nil {
		t.Fatalf("read scenario manifest: %v", err)
	}
	var declared manifestIntent
	if err := yaml.Unmarshal(intent, &declared); err != nil {
		t.Fatalf("decode scenario intent: %v", err)
	}
	temporary := t.TempDir()
	root := filepath.Join(temporary, "workspace")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	runner := commandRunner{binary: binary, home: filepath.Join(temporary, "home")}
	if err := os.WriteFile(filepath.Join(root, "lucy.yaml"), intent, 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Minute)
	defer cancel()
	if _, err := runner.execute(ctx, root, "install"); err != nil {
		t.Fatalf("fresh install: %v", err)
	}
	locked, err := readLock(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := scenario.verify(root, oracle, declared, locked); err != nil {
		t.Fatalf("installed runtime: %v", err)
	}
	t.Logf("install %s: generated lock and runtime outputs", id)
}
