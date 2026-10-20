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
	// manualSandbox marks sandboxes whose artifacts cannot be fetched, so the
	// suite skips them until the drop-in files exist.
	manualSandbox bool
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
	primary := got.Server.Primary
	equal("identity.eco", primary.Ecosystem, want.eco)
	equal("identity.name", primary.Name, want.name)
	equal("identity.version", primary.Version, want.version)
	if want.loaderEco != "" {
		component("loader.version", want.loaderEco, want.loaderVersion)
	}
	component("minecraft.version", "minecraft", want.minecraftVersion)
	if want.packageDirs && len(got.PackageDirs) == 0 {
		t.Error("package_dirs.present: no package paths")
	}
	if want.packageDirsSuffix != "" {
		first := ""
		if len(got.PackageDirs) > 0 {
			first = filepath.ToSlash(got.PackageDirs[0])
		}
		if !strings.HasSuffix(first, want.packageDirsSuffix) {
			t.Errorf("%s: got %q, want suffix %q", want.packageDirsCheck, first, want.packageDirsSuffix)
		}
	}
}

var probeScenarios = map[string]probeExpectation{
	"arclight-fabric": {
		name: "arclight", version: "arclight-1.21.1-1.0.1-8ec9529",
		loaderEco: "fabric", loaderVersion: "0.16.14", minecraftVersion: "1.21.1", packageDirs: true,
	},
	"arclight-forge": {
		name: "arclight", version: "arclight-1.21.1-1.0.1-8ec9529",
		loaderEco: "forge", loaderVersion: "52.1.1", minecraftVersion: "1.21.1", packageDirs: true,
	},
	"arclight-neoforge": {
		name: "arclight", version: "arclight-1.21.1-1.0.1-8ec9529",
		loaderEco: "neoforge", loaderVersion: "21.1.192", minecraftVersion: "1.21.1", packageDirs: true,
	},
	"fabric-executable-1214": {
		eco: "fabric", version: "0.16.9", minecraftVersion: "1.21.4", packageDirs: true,
	},
	"fabric-executable-262": {
		eco: "fabric", version: "0.19.3", minecraftVersion: "26.2", packageDirs: true,
	},
	"fabric-installed-1214": {
		eco: "fabric", version: "0.16.9", minecraftVersion: "1.21.4", manualSandbox: true,
	},
	"fabric-installed-262": {
		eco: "fabric", version: "0.19.3", minecraftVersion: "26.2", manualSandbox: true,
	},
	"forge-1201": {
		eco: "forge", version: "47.4.10", minecraftVersion: "1.20.1", packageDirs: true,
	},
	"forge-12111": {
		eco: "forge", version: "61.2.0", minecraftVersion: "1.21.11", packageDirs: true,
	},
	"mcdr-fabric": {
		eco: "fabric", version: "0.16.9", minecraftVersion: "1.21.4", packageDirs: true,
		packageDirsSuffix: "/server/mods", packageDirsCheck: "package_dirs.nested",
	},
	"mcdr-forge": {
		eco: "forge", version: "47.4.10", minecraftVersion: "1.20.1", packageDirs: true,
		packageDirsSuffix: "/server/mods", packageDirsCheck: "package_dirs.nested",
	},
	"mcdr-neoforge": {
		eco: "neoforge", version: "20.2.93", minecraftVersion: "1.20.2", packageDirs: true,
		packageDirsSuffix: "/server/mods", packageDirsCheck: "package_dirs.nested",
	},
	"neoforge-211": {
		eco: "neoforge", version: "21.1.248", minecraftVersion: "1.21.1", packageDirs: true,
	},
	"neoforge-262": {
		eco: "neoforge", version: "26.2.0.67", minecraftVersion: "1.26.2", packageDirs: true,
	},
	"paper": {
		name: "paper", minecraftVersion: "1.21.11", packageDirs: true,
		packageDirsSuffix: "/plugins", packageDirsCheck: "package_dirs.plugins",
	},
	"vanilla-1201": {
		eco: "minecraft", version: "1.20.1", minecraftVersion: "1.20.1",
	},
	"vanilla-262": {
		eco: "minecraft", version: "26.2", minecraftVersion: "26.2",
	},
}
