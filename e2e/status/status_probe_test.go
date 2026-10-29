//go:build e2e

package status

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type probeExpectation struct {
	eco               string
	name              string
	version           string
	minecraftVersion  string
	loaderEco         string
	loaderVersion     string
	packageDirs       bool
	packageDirsSuffix string
	packageDirsCheck  string
	// packages lists eco:name@version identifiers that discovery must report.
	packages []string
	// minPackages requires at least that many discovered packages. Use it for
	// packs whose complete package list is too large to pin.
	minPackages int
}

func (want probeExpectation) check(t *testing.T, got statusOutput) {
	t.Helper()
	equal := func(name, actual, expected string) {
		if expected != "" && actual != expected {
			t.Errorf("%s: got %q, want %q", name, actual, expected)
		}
	}
	component := func(name, eco, version string) {
		if slices.ContainsFunc(got.Server.Components, func(ref runtimeRef) bool {
			return ref.Ecosystem == eco && ref.Version == version
		}) {
			return
		}
		versions := []string{}
		for _, ref := range got.Server.Components {
			if ref.Ecosystem == eco {
				versions = append(versions, ref.Version)
			}
		}
		t.Errorf("%s: got %q, want %q", name, versions, version)
	}
	primary := got.Server.Product
	equal("identity.eco", primary.Ecosystem, want.eco)
	equal("identity.name", primary.Name, want.name)
	equal("identity.version", primary.Version, want.version)
	if want.loaderEco != "" {
		component("loader.version", want.loaderEco, want.loaderVersion)
	}
	component("minecraft.version", "minecraft", want.minecraftVersion)
	directories := got.Server.PackageDirectories
	if want.packageDirs && len(directories) == 0 {
		t.Error("package directories: no package paths")
	}
	if want.packageDirsSuffix != "" {
		first := ""
		if len(directories) > 0 {
			first = "/" + strings.TrimPrefix(filepath.ToSlash(directories[0]), "/")
		}
		if !strings.HasSuffix(first, want.packageDirsSuffix) {
			t.Errorf("%s: got %q, want suffix %q", want.packageDirsCheck, first, want.packageDirsSuffix)
		}
	}
	if len(want.packages) > 0 {
		found := map[string]struct{}{}
		gotIDs := make([]string, 0, len(got.Packages))
		for _, pkg := range got.Packages {
			found[pkg.identifier()] = struct{}{}
			gotIDs = append(gotIDs, pkg.identifier())
		}
		slices.Sort(gotIDs)
		for _, id := range want.packages {
			if _, ok := found[id]; !ok {
				t.Errorf("packages: missing %q; got %v", id, gotIDs)
			}
		}
	}
	if len(got.Packages) < want.minPackages {
		t.Errorf("packages: discovered %d, want at least %d", len(got.Packages), want.minPackages)
	}
}

// probeScenarios keeps the real-artifact smoke coverage that only the binary
// can prove: each retained sandbox exercises a distinct detector family, loader
// layout, or package-observation risk. Deterministic selection, identity
// merging, package projection, and the live-status document are covered
// in-process by workspace/discovery, workspace, and internal/cli.
var probeScenarios = map[string]probeExpectation{
	// Hybrid platforms project the bundled loader as a component.
	"arclight-fabric": {
		name: "arclight", version: "arclight-1.21.1-1.0.1-8ec9529",
		loaderEco: "fabric", loaderVersion: "0.16.14", minecraftVersion: "1.21.1", packageDirs: true,
	},
	"arclight-forge": {
		name: "arclight", version: "arclight-1.21.1-1.0.1-8ec9529",
		loaderEco: "forge", loaderVersion: "52.1.1", minecraftVersion: "1.21.1", packageDirs: true,
	},
	// A Fabric meta launcher before its first run.
	"fabric-executable-1214": {
		eco: "fabric", version: "0.16.9", minecraftVersion: "1.21.4", packageDirs: true,
	},
	// Real Fabric modpack content: package identity and counts.
	"cobblemon-1211": {
		eco: "fabric", version: "0.19.5", minecraftVersion: "1.21.1", packageDirs: true,
		minPackages: 30,
		packages:    []string{"fabric:cobblemon@1.8.2+1.21.1", "fabric:fabric-api@0.116.17+1.21.1"},
	},
	// A large NeoForge modpack exercises package projection at scale.
	"create-plus-1211": {
		eco: "neoforge", version: "21.1.233", minecraftVersion: "1.21.1", packageDirs: true,
		minPackages: 100,
		packages:    []string{"neoforge:alloyed@3.0.9+1.21.1"},
	},
	// A real Forge installer layout with argument sidecars.
	"forge-1201": {
		eco: "forge", version: "47.4.10", minecraftVersion: "1.20.1", packageDirs: true,
	},
	// NeoForge 26.2.0.67 installs declare Minecraft 26.2 in install_profile.json
	// and version.json; the loader tuple does not imply a 1.26.2 game version.
	"neoforge-262": {
		eco: "neoforge", version: "26.2.0.67", minecraftVersion: "26.2", packageDirs: true,
	},
	// The MCDR anchor moves the server directory under a wrapper.
	"mcdr-fabric": {
		eco: "fabric", version: "0.16.9", minecraftVersion: "1.21.4", packageDirs: true,
		packageDirsSuffix: "/server/mods", packageDirsCheck: "package_dirs.nested",
	},
	// A Paperclip jar reports plugins/ as the content directory.
	"paper": {
		name: "paper", version: "130", minecraftVersion: "1.21.11", packageDirs: true,
		packageDirsSuffix: "/plugins", packageDirsCheck: "package_dirs.plugins",
	},
	// Plain vanilla reports no content directory.
	"vanilla-262": {
		eco: "minecraft", version: "26.2", minecraftVersion: "26.2",
	},
}
