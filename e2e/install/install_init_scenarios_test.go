//go:build e2e

package install

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// initInstallScenario pins the runtime identity that initialization detects in
// a generated sandbox and the files that installation must produce from the
// initialized lock.
type initInstallScenario struct {
	product     string
	minecraft   string
	coreVersion string
	minPackages int
	// coreOutput names the installed file that holds the locked server artifact.
	// Installer-based products generate their core files, so they leave it empty.
	coreOutput      string
	requiredFiles   []string
	requiredInputs  []string
	launchArguments string
}

var initInstallScenarios = map[string]initInstallScenario{
	"vanilla-1201": {
		product:       "minecraft",
		minecraft:     "1.20.1",
		coreVersion:   "1.20.1",
		coreOutput:    "server.jar",
		requiredFiles: []string{"server.jar"},
	},
	"cobblemon-1211": {
		product:     "fabric",
		minecraft:   "1.21.1",
		coreVersion: "0.19.5",
		minPackages: 30,
		coreOutput:  "libraries/net/fabricmc/fabric-loader/0.19.5/fabric-loader-0.19.5.jar",
		requiredFiles: []string{
			"run.sh",
			"run.bat",
			"fabric-server-launcher.properties",
			"server.jar",
		},
	},
	"forge-1201": {
		product:         "forge",
		minecraft:       "1.20.1",
		coreVersion:     "47.4.10",
		requiredFiles:   []string{"run.sh", "user_jvm_args.txt"},
		requiredInputs:  []string{"libraries/net/minecraftforge/forge/1.20.1-47.4.10/forge-1.20.1-47.4.10-universal.jar"},
		launchArguments: "libraries/net/minecraftforge/forge/1.20.1-47.4.10/unix_args.txt",
	},
	"neoforge-211": {
		product:         "neoforge",
		minecraft:       "1.21.1",
		coreVersion:     "21.1.248",
		requiredFiles:   []string{"run.sh", "user_jvm_args.txt"},
		requiredInputs:  []string{"libraries/net/neoforged/neoforge/21.1.248/neoforge-21.1.248-universal.jar"},
		launchArguments: "libraries/net/neoforged/neoforge/21.1.248/unix_args.txt",
	},
	"paper": {
		product:        "paper",
		minecraft:      "1.21.11",
		coreVersion:    "130",
		coreOutput:     "server.jar",
		requiredFiles:  []string{"server.jar", "versions/1.21.11/paper-1.21.11.jar"},
		requiredInputs: []string{"cache/mojang_1.21.11.jar"},
	},
	"purpur": {
		product:        "purpur",
		minecraft:      "1.21.11",
		coreVersion:    "2568",
		coreOutput:     "server.jar",
		requiredFiles:  []string{"server.jar", "versions/1.21.11/purpur-1.21.11.jar"},
		requiredInputs: []string{"cache/mojang_1.21.11.jar"},
	},
}

func (scenario initInstallScenario) verifyInitialized(
	declared manifestIntent,
	locked lockDocument,
) error {
	if scenario.coreVersion == "" {
		return fmt.Errorf("init-install scenario must pin the exact core version")
	}
	server := declared.Server
	if server.Minecraft != scenario.minecraft || server.Core.Product != scenario.product {
		return fmt.Errorf(
			"manifest identity: got %s/%s, want %s/%s",
			server.Minecraft, server.Core.Product, scenario.minecraft, scenario.product,
		)
	}
	if server.Core.ProductVersion != scenario.coreVersion {
		return fmt.Errorf(
			"manifest core version: got %s, want %s",
			server.Core.ProductVersion, scenario.coreVersion,
		)
	}
	lockedServer := locked.Server
	if lockedServer.Minecraft != scenario.minecraft || lockedServer.Product != scenario.product {
		return fmt.Errorf(
			"lock identity: got %s/%s, want %s/%s",
			lockedServer.Minecraft, lockedServer.Product, scenario.minecraft, scenario.product,
		)
	}
	if lockedServer.CoreVersion != scenario.coreVersion {
		return fmt.Errorf(
			"lock core version: got %s, want %s",
			lockedServer.CoreVersion, scenario.coreVersion,
		)
	}
	if lockedServer.Directory != server.Directory {
		return fmt.Errorf("lock directory does not match the generated manifest")
	}
	if len(locked.Packages) < scenario.minPackages {
		return fmt.Errorf(
			"lock packages: got %d, want at least %d",
			len(locked.Packages), scenario.minPackages,
		)
	}
	return nil
}

func (scenario initInstallScenario) verifyInstalled(root string, locked lockDocument) error {
	live := filepath.Join(root, filepath.FromSlash(locked.Server.Directory))
	if scenario.coreOutput != "" {
		path := filepath.Join(live, filepath.FromSlash(scenario.coreOutput))
		if err := verifyLockedFile(path, locked.Server.lockedArtifact); err != nil {
			return fmt.Errorf("server core: %w", err)
		}
	}
	if err := verifyBootstrapInputs(live, locked.Server.Inputs, scenario.requiredInputs); err != nil {
		return err
	}
	if err := verifyRuntimeFiles(live, installScenario{
		product:         scenario.product,
		requiredFiles:   scenario.requiredFiles,
		launchArguments: scenario.launchArguments,
	}); err != nil {
		return err
	}
	if err := verifyInitInstallPackages(live, locked.Packages); err != nil {
		return err
	}
	receipt := filepath.Join(root, ".lucy", "install.yaml")
	if _, err := os.Stat(receipt); err != nil {
		return fmt.Errorf("installation receipt: %w", err)
	}
	return nil
}

func verifyInitInstallPackages(live string, packages []lockedPackage) error {
	installed := 0
	for _, pkg := range packages {
		if !pkg.enabled() {
			continue
		}
		installed++
		path := filepath.Join(live, pkg.folder(), pkg.Filename)
		if err := verifyLockedFile(path, pkg.lockedArtifact); err != nil {
			return fmt.Errorf("package %s: %w", pkg.ProjectID, err)
		}
	}
	if installed == 0 && len(packages) > 0 {
		return fmt.Errorf("lock recorded %d packages but none are enabled", len(packages))
	}
	return nil
}

func (pkg lockedPackage) enabled() bool {
	if len(pkg.Requirements) == 0 {
		return true
	}
	return slices.ContainsFunc(pkg.Requirements, func(requirement lockedRequirement) bool {
		return requirement.Enabled
	})
}

func (pkg lockedPackage) folder() string {
	if pkg.Environment == "mcdr" {
		return "plugins"
	}
	switch pkg.Ecosystem {
	case "bukkit", "paper":
		return "plugins"
	default:
		return "mods"
	}
}
