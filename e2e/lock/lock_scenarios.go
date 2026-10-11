package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type lockScenario struct {
	environment      string
	distribution     string
	minecraft        string
	coreVersion      string
	provider         string
	projectID        string
	filename         string
	bootstrap        string
	coreOutput       string
	coreFixture      string
	minecraftFixture string
	requiredFiles    []string
	requiredInputs   []string
	launchArguments  string
	runtimeArtifacts []runtimeArtifactExpectation
	packages         []packageExpectation
}

type runtimeArtifactExpectation struct {
	fixture, runtime string
}

type packageExpectation struct {
	reference, projectID, releaseID, fileID, filename string
}

var lockScenarios = map[string]lockScenario{
	"vanilla-1201": {
		environment:      "vanilla-1201",
		distribution:     "vanilla",
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
		environment:      "lock-fabric-1214",
		distribution:     "fabric",
		minecraft:        "1.21.4",
		coreVersion:      "0.16.9",
		provider:         "fabric",
		projectID:        "fabric-loader",
		filename:         "fabric-loader-0.16.9.jar",
		bootstrap:        "fabric",
		coreOutput:       "libraries/net/fabricmc/fabric-loader/0.16.9/fabric-loader-0.16.9.jar",
		minecraftFixture: "server.jar",
		requiredFiles: []string{
			"run.sh",
			"run.bat",
			"fabric-server-launcher.properties",
		},
		packages: []packageExpectation{
			{
				reference: "modrinth:fabric-api", projectID: "P7dR8mSH", releaseID: "p96k10UR",
				fileID: "9aQezMA2", filename: "fabric-api-0.119.4+1.21.4.jar",
			},
			{
				reference: "modrinth:flan", projectID: "Si383TIH", releaseID: "9EC25rAV",
				fileID: "HjsqW7BQ", filename: "flan-1.21.4-1.11.7.b-fabric.jar",
			},
		},
	},
	"forge-1201": {
		environment:     "forge-1201",
		distribution:    "forge",
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
			fixture: "libraries/net/minecraftforge/forge/1.20.1-47.4.10/forge-1.20.1-47.4.10-universal.jar",
			runtime: "libraries/net/minecraftforge/forge/1.20.1-47.4.10/forge-1.20.1-47.4.10-universal.jar",
		}},
	},
	"neoforge-211": {
		environment:     "neoforge-211",
		distribution:    "neoforge",
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
			fixture: "libraries/net/neoforged/neoforge/21.1.248/neoforge-21.1.248-universal.jar",
			runtime: "libraries/net/neoforged/neoforge/21.1.248/neoforge-21.1.248-universal.jar",
		}},
	},
}

func executeScenario(ctx context.Context, binary, sandboxes, id string) (resultErr error) {
	scenario, ok := lockScenarios[id]
	if !ok {
		return fmt.Errorf("unknown install scenario %q", id)
	}
	fixture := filepath.Join(sandboxes, scenario.environment)
	info, err := os.Stat(fixture)
	if err != nil {
		return fmt.Errorf("generate %s with task envs:gen first: %w", scenario.environment, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("environment fixture is not a directory: %s", fixture)
	}
	intent, err := os.ReadFile(filepath.Join("e2e", "testdata", "lock", id+".yaml"))
	if err != nil {
		return fmt.Errorf("read scenario intent: %w", err)
	}
	var declared manifestIntent
	if err := yaml.Unmarshal(intent, &declared); err != nil {
		return fmt.Errorf("decode scenario intent: %w", err)
	}
	temporary, err := os.MkdirTemp("", "lucy-e2e-install-"+id+"-*")
	if err != nil {
		return err
	}
	defer func() {
		resultErr = errors.Join(resultErr, os.RemoveAll(temporary))
	}()
	root := filepath.Join(temporary, "workspace")
	if err := os.Mkdir(root, 0o755); err != nil {
		return err
	}
	runner := commandRunner{binary: binary, home: filepath.Join(temporary, "home")}
	if err := os.WriteFile(filepath.Join(root, "lucy.yaml"), intent, 0o644); err != nil {
		return err
	}
	if _, err := runner.execute(ctx, root, "install"); err != nil {
		return fmt.Errorf("fresh install: %w", err)
	}
	locked, err := readLock(root)
	if err != nil {
		return err
	}
	if err := scenario.verify(root, fixture, declared, locked); err != nil {
		return fmt.Errorf("installed runtime: %w", err)
	}
	fmt.Printf("install %s: generated lock and runtime outputs\n", id)
	return nil
}
