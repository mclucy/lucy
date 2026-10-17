package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/e2e/internal/testkit"
	"gopkg.in/yaml.v3"
)

type lockScenario struct {
	// sandbox names the generated server that supplies pinned oracle bytes.
	// Scenarios without one pin their own digests below, so fixtures that are
	// not real servers never need a generated directory or a committed binary.
	sandbox string
	// pending marks a scenario whose core Lucy cannot install yet. The suite
	// reports it as skipped, keeping the gap visible without failing the run.
	pending          string
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
	// reference names the manifest selector that pins the package. An empty
	// reference marks a package only a transitive requirement pulled in, and
	// then loader carries the ecosystem it was resolved for.
	reference                                      string
	loader                                         string
	projectID, releaseID, fileID, filename, sha256 string
	modules                                        []moduleExpectation
}

// moduleExpectation pins a native module a locked package must carry. An empty
// version asserts that the module version equals its package version.
type moduleExpectation struct {
	id         string
	version    string
	member     string
	provides   string
	dependency *moduleDependencyExpectation
}

type moduleDependencyExpectation struct {
	id, kind, constraint string
}

var lockScenarios = map[string]lockScenario{
	"vanilla-1201": {
		sandbox:          "vanilla-1201",
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
	"fabric-1165": {
		distribution:    "fabric",
		minecraft:       "1.16.5",
		coreVersion:     "0.19.5",
		provider:        "fabric",
		projectID:       "fabric-loader",
		filename:        "fabric-loader-0.19.5.jar",
		bootstrap:       "fabric",
		coreOutput:      "libraries/net/fabricmc/fabric-loader/0.19.5/fabric-loader-0.19.5.jar",
		minecraftDigest: "58f329c7d2696526f948470aa6fd0b45545039b64cb75015e64c12194b373da6",
		requiredFiles: []string{
			"run.sh",
			"run.bat",
			"fabric-server-launcher.properties",
		},
	},
	"fabric-118": {
		distribution:    "fabric",
		minecraft:       "1.18",
		coreVersion:     "0.19.5",
		provider:        "fabric",
		projectID:       "fabric-loader",
		filename:        "fabric-loader-0.19.5.jar",
		bootstrap:       "fabric",
		coreOutput:      "libraries/net/fabricmc/fabric-loader/0.19.5/fabric-loader-0.19.5.jar",
		minecraftDigest: "8417edceb471b38a59a6ce260f3aa1479e95264a38eb56283bdf24ec98647e6f",
		requiredFiles: []string{
			"run.sh",
			"run.bat",
			"fabric-server-launcher.properties",
		},
	},
	"fabric-1214": {
		distribution:    "fabric",
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
				modules: []moduleExpectation{
					{id: "fabric-api", provides: "fabric"},
					{
						id: "fabric-api-base", version: "0.4.54+b47eab6b04",
						member: "META-INF/jars/fabric-api-base-0.4.54+b47eab6b04.jar",
					},
				},
			},
			{
				reference: "modrinth:flan", projectID: "Si383TIH", releaseID: "9EC25rAV",
				fileID: "HjsqW7BQ", filename: "flan-1.21.4-1.11.7.b-fabric.jar",
				sha256: "33c466b7c319f1597798f62d13b619317590bf069c4a2c11715fb19fdd98e7f8",
				modules: []moduleExpectation{
					{
						id: "flan", version: "1.21.4-1.11.7.b",
						dependency: &moduleDependencyExpectation{id: "fabric", kind: "required", constraint: ">=0.119.2+1.21.4"},
					},
					{
						id: "lingua_bib", version: "1.21.4-1.0.3",
						member: "META-INF/jars/lingua_bib-1.21.4-1.0.3-fabric.jar",
					},
				},
			},
		},
	},
	// Pre-1.18 Forge installers embed no launcher scripts; the forge-installer
	// bootstrap only completes the modern layout, so these scenarios stay
	// pending until legacy installs land.
	"forge-1122": {
		sandbox:      "forge-1122",
		pending:      "pre-1.18 launch layout: the installer embeds no launcher scripts",
		distribution: "forge",
		minecraft:    "1.12.2",
		coreVersion:  "14.23.5.2859",
		provider:     "forge",
		projectID:    "forge",
		filename:     "forge-1.12.2-14.23.5.2859-installer.jar",
		bootstrap:    "forge-installer",
		coreFixture:  "forge-1.12.2-14.23.5.2859-installer.jar",
	},
	"forge-1144": {
		sandbox:      "forge-1144",
		pending:      "pre-1.18 launch layout: the installer embeds no launcher scripts",
		distribution: "forge",
		minecraft:    "1.14.4",
		coreVersion:  "28.2.26",
		provider:     "forge",
		projectID:    "forge",
		filename:     "forge-1.14.4-28.2.26-installer.jar",
		bootstrap:    "forge-installer",
		coreFixture:  "forge-1.14.4-28.2.26-installer.jar",
	},
	"forge-1165": {
		sandbox:      "forge-1165",
		pending:      "pre-1.18 launch layout: the installer embeds no launcher scripts",
		distribution: "forge",
		minecraft:    "1.16.5",
		coreVersion:  "36.2.34",
		provider:     "forge",
		projectID:    "forge",
		filename:     "forge-1.16.5-36.2.34-installer.jar",
		bootstrap:    "forge-installer",
		coreFixture:  "forge-1.16.5-36.2.34-installer.jar",
	},
	"forge-1710": {
		sandbox:      "forge-1710",
		pending:      "legacy installer profile and pre-1.18 launch layout",
		distribution: "forge",
		minecraft:    "1.7.10",
		coreVersion:  "10.13.4.1614",
		provider:     "forge",
		projectID:    "forge",
		filename:     "forge-1.7.10-10.13.4.1614-1.7.10-installer.jar",
		bootstrap:    "forge-installer",
		coreFixture:  "forge-1.7.10-10.13.4.1614-1.7.10-installer.jar",
	},
	"forge-118": {
		sandbox:         "forge-118",
		distribution:    "forge",
		minecraft:       "1.18",
		coreVersion:     "38.0.17",
		provider:        "forge",
		projectID:       "forge",
		filename:        "forge-1.18-38.0.17-installer.jar",
		bootstrap:       "forge-installer",
		coreFixture:     "forge-1.18-38.0.17-installer.jar",
		requiredFiles:   []string{"run.sh", "user_jvm_args.txt"},
		requiredInputs:  []string{"libraries/net/minecraftforge/forge/1.18-38.0.17/forge-1.18-38.0.17-universal.jar"},
		launchArguments: "libraries/net/minecraftforge/forge/1.18-38.0.17/unix_args.txt",
		runtimeArtifacts: []runtimeArtifactExpectation{{
			sandboxFile: "libraries/net/minecraftforge/forge/1.18-38.0.17/forge-1.18-38.0.17-universal.jar",
			runtimeFile: "libraries/net/minecraftforge/forge/1.18-38.0.17/forge-1.18-38.0.17-universal.jar",
		}},
	},
	"forge-1192": {
		sandbox:         "forge-1192",
		distribution:    "forge",
		minecraft:       "1.19.2",
		coreVersion:     "43.5.0",
		provider:        "forge",
		projectID:       "forge",
		filename:        "forge-1.19.2-43.5.0-installer.jar",
		bootstrap:       "forge-installer",
		coreFixture:     "forge-1.19.2-43.5.0-installer.jar",
		requiredFiles:   []string{"run.sh", "user_jvm_args.txt"},
		requiredInputs:  []string{"libraries/net/minecraftforge/forge/1.19.2-43.5.0/forge-1.19.2-43.5.0-universal.jar"},
		launchArguments: "libraries/net/minecraftforge/forge/1.19.2-43.5.0/unix_args.txt",
		runtimeArtifacts: []runtimeArtifactExpectation{{
			sandboxFile: "libraries/net/minecraftforge/forge/1.19.2-43.5.0/forge-1.19.2-43.5.0-universal.jar",
			runtimeFile: "libraries/net/minecraftforge/forge/1.19.2-43.5.0/forge-1.19.2-43.5.0-universal.jar",
		}},
	},
	"forge-1201": {
		sandbox:         "forge-1201",
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
			sandboxFile: "libraries/net/minecraftforge/forge/1.20.1-47.4.10/forge-1.20.1-47.4.10-universal.jar",
			runtimeFile: "libraries/net/minecraftforge/forge/1.20.1-47.4.10/forge-1.20.1-47.4.10-universal.jar",
		}},
	},
	"forge-1211": {
		sandbox:       "forge-1211",
		distribution:  "forge",
		minecraft:     "1.21.1",
		coreVersion:   "52.1.0",
		provider:      "forge",
		projectID:     "forge",
		filename:      "forge-1.21.1-52.1.0-installer.jar",
		bootstrap:     "forge-installer",
		coreFixture:   "forge-1.21.1-52.1.0-installer.jar",
		requiredFiles: []string{"run.sh", "user_jvm_args.txt"},
		requiredInputs: []string{
			"libraries/net/minecraftforge/forge/1.21.1-52.1.0/forge-1.21.1-52.1.0-shim.jar",
			"libraries/net/minecraftforge/forge/1.21.1-52.1.0/forge-1.21.1-52.1.0-universal.jar",
		},
		launchArguments: "libraries/net/minecraftforge/forge/1.21.1-52.1.0/unix_args.txt",
		runtimeArtifacts: []runtimeArtifactExpectation{
			{
				sandboxFile: "libraries/net/minecraftforge/forge/1.21.1-52.1.0/forge-1.21.1-52.1.0-shim.jar",
				runtimeFile: "libraries/net/minecraftforge/forge/1.21.1-52.1.0/forge-1.21.1-52.1.0-shim.jar",
			},
			{
				sandboxFile: "libraries/net/minecraftforge/forge/1.21.1-52.1.0/forge-1.21.1-52.1.0-universal.jar",
				runtimeFile: "libraries/net/minecraftforge/forge/1.21.1-52.1.0/forge-1.21.1-52.1.0-universal.jar",
			},
		},
	},
	"forge-2612": {
		sandbox:       "forge-2612",
		distribution:  "forge",
		minecraft:     "26.1.2",
		coreVersion:   "64.1.0",
		provider:      "forge",
		projectID:     "forge",
		filename:      "forge-26.1.2-64.1.0-installer.jar",
		bootstrap:     "forge-installer",
		coreFixture:   "forge-26.1.2-64.1.0-installer.jar",
		requiredFiles: []string{"run.sh", "user_jvm_args.txt"},
		requiredInputs: []string{
			"libraries/net/minecraftforge/forge/26.1.2-64.1.0/forge-26.1.2-64.1.0-shim.jar",
			"libraries/net/minecraftforge/forge/26.1.2-64.1.0/forge-26.1.2-64.1.0-universal.jar",
		},
		launchArguments: "libraries/net/minecraftforge/forge/26.1.2-64.1.0/unix_args.txt",
		runtimeArtifacts: []runtimeArtifactExpectation{
			{
				sandboxFile: "libraries/net/minecraftforge/forge/26.1.2-64.1.0/forge-26.1.2-64.1.0-shim.jar",
				runtimeFile: "libraries/net/minecraftforge/forge/26.1.2-64.1.0/forge-26.1.2-64.1.0-shim.jar",
			},
			{
				sandboxFile: "libraries/net/minecraftforge/forge/26.1.2-64.1.0/forge-26.1.2-64.1.0-universal.jar",
				runtimeFile: "libraries/net/minecraftforge/forge/26.1.2-64.1.0/forge-26.1.2-64.1.0-universal.jar",
			},
		},
	},
	"neoforge-202": {
		sandbox:         "neoforge-202",
		distribution:    "neoforge",
		minecraft:       "1.20.2",
		coreVersion:     "20.2.93",
		provider:        "neoforge",
		projectID:       "neoforge",
		filename:        "neoforge-20.2.93-installer.jar",
		bootstrap:       "forge-installer",
		coreFixture:     "neoforge-20.2.93-installer.jar",
		requiredFiles:   []string{"run.sh", "user_jvm_args.txt"},
		requiredInputs:  []string{"libraries/net/neoforged/neoforge/20.2.93/neoforge-20.2.93-universal.jar"},
		launchArguments: "libraries/net/neoforged/neoforge/20.2.93/unix_args.txt",
		runtimeArtifacts: []runtimeArtifactExpectation{{
			sandboxFile: "libraries/net/neoforged/neoforge/20.2.93/neoforge-20.2.93-universal.jar",
			runtimeFile: "libraries/net/neoforged/neoforge/20.2.93/neoforge-20.2.93-universal.jar",
		}},
	},
	"neoforge-211": {
		sandbox:         "neoforge-211",
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
			sandboxFile: "libraries/net/neoforged/neoforge/21.1.248/neoforge-21.1.248-universal.jar",
			runtimeFile: "libraries/net/neoforged/neoforge/21.1.248/neoforge-21.1.248-universal.jar",
		}},
	},
	"neoforge-2612": {
		sandbox:         "neoforge-2612",
		distribution:    "neoforge",
		minecraft:       "26.1.2",
		coreVersion:     "26.1.2.122",
		provider:        "neoforge",
		projectID:       "neoforge",
		filename:        "neoforge-26.1.2.122-installer.jar",
		bootstrap:       "forge-installer",
		coreFixture:     "neoforge-26.1.2.122-installer.jar",
		requiredFiles:   []string{"run.sh", "user_jvm_args.txt"},
		requiredInputs:  []string{"libraries/net/neoforged/neoforge/26.1.2.122/neoforge-26.1.2.122-universal.jar"},
		launchArguments: "libraries/net/neoforged/neoforge/26.1.2.122/unix_args.txt",
		runtimeArtifacts: []runtimeArtifactExpectation{{
			sandboxFile: "libraries/net/neoforged/neoforge/26.1.2.122/neoforge-26.1.2.122-universal.jar",
			runtimeFile: "libraries/net/neoforged/neoforge/26.1.2.122/neoforge-26.1.2.122-universal.jar",
		}},
	},
	"kilt-211": {
		sandbox:         "kilt-211",
		distribution:    "fabric",
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
		packages: []packageExpectation{
			{
				reference: "modrinth:kilt", projectID: "t87XZEqv", releaseID: "FVVpTWMd",
				fileID: "ygY8FKVX", filename: "Kilt-21.1.15.jar",
				modules: []moduleExpectation{
					{
						id: "kilt", version: "21.1.15", provides: "neoforge",
						dependency: &moduleDependencyExpectation{
							id: "fabric-language-kotlin", kind: "required", constraint: ">=1.13.12+kotlin.2.4.0",
						},
					},
					{
						id: "mm_kilt", version: "2.5.3",
						member: "META-INF/jars/Fabric-ASM-2.5.3.jar",
					},
				},
			},
			{
				reference: "modrinth:fabric-api", projectID: "P7dR8mSH", releaseID: "Mys3P7lK",
				fileID: "WuOG3t7I", filename: "fabric-api-0.116.17+1.21.1.jar",
				modules: []moduleExpectation{
					{id: "fabric-api", provides: "fabric"},
					{
						id: "fabric-api-base", version: "0.4.42+6573ed8c19",
						member: "META-INF/jars/fabric-api-base-0.116.17.jar",
					},
				},
			},
			{
				reference: "modrinth:architectury-api", projectID: "lhGA9TYQ", releaseID: "Pzc2FP5K",
				fileID: "CCNv1IEC", filename: "architectury-13.0.11-fabric.jar",
				modules: []moduleExpectation{{
					id: "architectury", version: "13.0.11",
					dependency: &moduleDependencyExpectation{id: "minecraft", kind: "required", constraint: "~1.21-"},
				}},
			},
			{
				// Kilt metadata requires the Kotlin runtime, which no manifest
				// selector pins, so resolution must pull it on its own.
				loader:    "fabric",
				projectID: "Ha28R6CL", releaseID: "SKW1BTL3", fileID: "MktD9PnE",
				filename: "fabric-language-kotlin-1.14.2+kotlin.2.4.21.jar",
				sha256:   "fbdbf5286ab9d65fedb49e6e01c15e338ae303eb7b9d8c7423d0a9bcfe526ef9",
				modules: []moduleExpectation{
					{
						id: "fabric-language-kotlin", version: "1.14.2+kotlin.2.4.21",
						dependency: &moduleDependencyExpectation{id: "fabricloader", kind: "required", constraint: ">=0.19.5"},
					},
					{
						id: "org_jetbrains_kotlin_kotlin-stdlib", version: "2.4.21",
						member: "META-INF/jars/kotlin-stdlib-2.4.21.jar",
					},
				},
			},
		},
	},
	"connector-211": {
		sandbox:         "connector-211",
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
			sandboxFile: "libraries/net/neoforged/neoforge/21.1.248/neoforge-21.1.248-universal.jar",
			runtimeFile: "libraries/net/neoforged/neoforge/21.1.248/neoforge-21.1.248-universal.jar",
		}},
		packages: []packageExpectation{
			{
				reference: "modrinth:connector", projectID: "u58R1TMW", releaseID: "IITF0PRC",
				fileID: "U2fKggY2", filename: "connector-2.0.0-beta.17+1.21.1-full.jar",
				modules: []moduleExpectation{{
					id: "connector", version: "2.0.0-beta.17+1.21.1",
					member:     "META-INF/jarjar/org.sinytra.connector-2.0.0-beta.17+1.21.1-mod.jar",
					dependency: &moduleDependencyExpectation{id: "neoforge", kind: "required", constraint: "[21.1.97,)"},
				}},
			},
			{
				reference: "modrinth:forgified-fabric-api", projectID: "Aqlf1Shp", releaseID: "C4eskNuR",
				fileID: "WtX00PCF", filename: "forgified-fabric-api-0.116.15+2.3.6+1.21.1.jar",
				modules: []moduleExpectation{
					{
						id: "fabric_api", version: "0.116.15+2.3.6+1.21.1",
						dependency: &moduleDependencyExpectation{id: "neoforge", kind: "required", constraint: "[21.1.169,)"},
					},
					{
						id: "fabric_api_base", version: "0.4.42+d1308ded19",
						member: "META-INF/jars/fabric-api-base-0.4.42+d1308ded19.jar",
					},
				},
			},
			{
				reference: "modrinth:architectury-api", projectID: "lhGA9TYQ", releaseID: "1IiqEQGl",
				fileID: "7Ni3RtRU", filename: "architectury-13.0.11-neoforge.jar",
				modules: []moduleExpectation{{
					id: "architectury", version: "13.0.11",
					dependency: &moduleDependencyExpectation{id: "neoforge", kind: "required", constraint: "[21.0.110-beta,)"},
				}},
			},
		},
	},
}

func executeScenario(ctx context.Context, binary, sandboxes, id string) (resultErr error) {
	scenario, ok := lockScenarios[id]
	if !ok {
		return fmt.Errorf("unknown install scenario %q", id)
	}
	if scenario.pending != "" {
		return testkit.Skipped{Reason: scenario.pending}
	}
	oracle := ""
	if scenario.sandbox != "" {
		oracle = filepath.Join(sandboxes, scenario.sandbox)
		info, err := os.Stat(oracle)
		if err != nil {
			return fmt.Errorf("generate %s with task sandboxes:gen first: %w", scenario.sandbox, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("sandbox is not a directory: %s", oracle)
		}
	}
	intent, err := os.ReadFile(filepath.Join("e2e", "lock", "testdata", id+".yaml"))
	if err != nil {
		return fmt.Errorf("read scenario manifest: %w", err)
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
	if err := scenario.verify(root, oracle, declared, locked); err != nil {
		return fmt.Errorf("installed runtime: %w", err)
	}
	fmt.Printf("install %s: generated lock and runtime outputs\n", id)
	return nil
}
