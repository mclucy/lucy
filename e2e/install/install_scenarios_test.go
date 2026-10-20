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
}

type runtimeArtifactExpectation struct {
	sandboxFile, runtimeFile string
}

type packageExpectation struct {
	reference, projectID, releaseID, fileID, filename, sha256 string
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
				reference: "modrinth:fabric-api", projectID: "P7dR8mSH", releaseID: "p96k10UR",
				fileID: "9aQezMA2", filename: "fabric-api-0.119.4+1.21.4.jar",
				sha256: "d183bacb845167f09264c2f90322b7ecffe8826debda6f60e597889264bef4af",
			},
			{
				reference: "modrinth:flan", projectID: "Si383TIH", releaseID: "9EC25rAV",
				fileID: "HjsqW7BQ", filename: "flan-1.21.4-1.11.7.b-fabric.jar",
				sha256: "33c466b7c319f1597798f62d13b619317590bf069c4a2c11715fb19fdd98e7f8",
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
}

func executeScenario(t *testing.T, binary, id string) {
	t.Helper()
	scenario := installScenarios[id]
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
