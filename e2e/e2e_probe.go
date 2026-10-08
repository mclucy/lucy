package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

type probeExpectation struct {
	eco              string
	name             string
	version          string
	minecraftVersion string
	loaderEco        string
	loaderVersion    string
	modPath          bool
	modPathSuffix    string
	modPathCheck     string
}

func (want probeExpectation) check(got statusOutput) []string {
	failures := []string{}
	equal := func(name, actual, expected string) {
		if expected != "" && actual != expected {
			failures = append(failures, fmt.Sprintf("%s: got %q, want %q", name, actual, expected))
		}
	}
	component := func(name, eco, version string) {
		if slices.ContainsFunc(got.Server.RuntimeComponents, func(ref runtimeRef) bool {
			return ref.Eco == eco && ref.Version == version
		}) {
			return
		}
		versions := []string{}
		for _, ref := range got.Server.RuntimeComponents {
			if ref.Eco == eco {
				versions = append(versions, ref.Version)
			}
		}
		failures = append(failures, fmt.Sprintf("%s: got %q, want %q", name, versions, version))
	}
	primary := got.Server.PrimaryRuntime
	equal("identity.eco", primary.Eco, want.eco)
	equal("identity.name", primary.Name, want.name)
	equal("identity.version", primary.Version, want.version)
	if want.loaderEco != "" {
		component("loader.version", want.loaderEco, want.loaderVersion)
	}
	component("minecraft.version", "minecraft", want.minecraftVersion)
	if want.modPath && len(got.ModPath) == 0 {
		failures = append(failures, "mod_path.present: no package paths")
	}
	if want.modPathSuffix != "" {
		first := ""
		if len(got.ModPath) > 0 {
			first = filepath.ToSlash(got.ModPath[0])
		}
		if !strings.HasSuffix(first, want.modPathSuffix) {
			failures = append(failures, fmt.Sprintf("%s: got %q, want suffix %q", want.modPathCheck, first, want.modPathSuffix))
		}
	}
	return failures
}

// These pinned expectations mirror the artifacts in the envgen manifest.
// Optional fields represent checks that a scenario intentionally does not make.
var probeScenarios = map[string]probeExpectation{
	"arclight-fabric": {
		name: "arclight", version: "arclight-1.21.1-1.0.1-8ec9529",
		loaderEco: "fabric", loaderVersion: "0.16.14", minecraftVersion: "1.21.1", modPath: true,
	},
	"arclight-forge": {
		name: "arclight", version: "arclight-1.21.1-1.0.1-8ec9529",
		loaderEco: "forge", loaderVersion: "52.1.1", minecraftVersion: "1.21.1", modPath: true,
	},
	"arclight-neoforge": {
		name: "arclight", version: "arclight-1.21.1-1.0.1-8ec9529",
		loaderEco: "neoforge", loaderVersion: "21.1.192", minecraftVersion: "1.21.1", modPath: true,
	},
	"fabric-executable-1214": {
		eco: "fabric", version: "0.16.9", minecraftVersion: "1.21.4", modPath: true,
	},
	"fabric-executable-262": {
		eco: "fabric", version: "0.19.3", minecraftVersion: "26.2", modPath: true,
	},
	"fabric-installed-1214": {
		eco: "fabric", version: "0.16.9", minecraftVersion: "1.21.4",
	},
	"fabric-installed-262": {
		eco: "fabric", version: "0.19.3", minecraftVersion: "26.2",
	},
	"forge-1201": {
		eco: "forge", version: "47.4.10", minecraftVersion: "1.20.1", modPath: true,
	},
	"forge-12111": {
		eco: "forge", version: "61.2.0", minecraftVersion: "1.21.11", modPath: true,
	},
	"mcdr-fabric": {
		eco: "fabric", version: "0.16.9", minecraftVersion: "1.21.4", modPath: true,
		modPathSuffix: "/server/mods", modPathCheck: "mod_path.nested",
	},
	"mcdr-forge": {
		eco: "forge", version: "47.4.10", minecraftVersion: "1.20.1", modPath: true,
		modPathSuffix: "/server/mods", modPathCheck: "mod_path.nested",
	},
	"mcdr-neoforge": {
		eco: "neoforge", version: "20.2.93", minecraftVersion: "1.20.2", modPath: true,
		modPathSuffix: "/server/mods", modPathCheck: "mod_path.nested",
	},
	"neoforge-211": {
		eco: "neoforge", version: "21.1.248", minecraftVersion: "1.21.1", modPath: true,
	},
	"neoforge-262": {
		eco: "neoforge", version: "26.2.0.67", minecraftVersion: "1.26.2", modPath: true,
	},
	"paper": {
		name: "paper", minecraftVersion: "1.21.11", modPath: true,
		modPathSuffix: "/plugins", modPathCheck: "mod_path.plugins",
	},
	"vanilla-1201": {
		eco: "minecraft", version: "1.20.1", minecraftVersion: "1.20.1",
	},
	"vanilla-262": {
		eco: "minecraft", version: "26.2", minecraftVersion: "26.2",
	},
}
