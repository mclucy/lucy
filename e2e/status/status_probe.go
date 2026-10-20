package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
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
}

func (want probeExpectation) check(got statusOutput) []string {
	var failures []string
	equal := func(name, actual, expected string) {
		if expected != "" && actual != expected {
			failures = append(failures, fmt.Sprintf("%s: got %q, want %q", name, actual, expected))
		}
	}
	component := func(name, eco, version string) {
		if slices.ContainsFunc(got.Server.Components, func(ref runtimeRef) bool {
			return ref.Ecosystem == eco && ref.Version == version
		}) {
			return
		}
		var versions []string
		for _, ref := range got.Server.Components {
			if ref.Ecosystem == eco {
				versions = append(versions, ref.Version)
			}
		}
		failures = append(failures, fmt.Sprintf("%s: got %q, want %q", name, versions, version))
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
		failures = append(failures, "package_dirs.present: no package paths")
	}
	if want.packageDirsSuffix != "" {
		first := ""
		if len(got.PackageDirs) > 0 {
			first = filepath.ToSlash(got.PackageDirs[0])
		}
		if !strings.HasSuffix(first, want.packageDirsSuffix) {
			failures = append(failures, fmt.Sprintf("%s: got %q, want suffix %q", want.packageDirsCheck, first, want.packageDirsSuffix))
		}
	}
	return failures
}

var probeScenarios = map[string]probeExpectation{
	"arclight-fabric": {
		name:             "arclight",
		version:          "arclight-1.21.1-1.0.1-8ec9529",
		loaderEco:        "fabric",
		loaderVersion:    "0.16.14",
		minecraftVersion: "1.21.1",
		packageDirs:      true,
	},
	"arclight-forge": {
		name:             "arclight",
		version:          "arclight-1.21.1-1.0.1-8ec9529",
		loaderEco:        "forge",
		loaderVersion:    "52.1.1",
		minecraftVersion: "1.21.1",
		packageDirs:      true,
	},
	"arclight-neoforge": {
		name:             "arclight",
		version:          "arclight-1.21.1-1.0.1-8ec9529",
		loaderEco:        "neoforge",
		loaderVersion:    "21.1.192",
		minecraftVersion: "1.21.1",
		packageDirs:      true,
	},
	"fabric-executable-1214": {
		eco:              "fabric",
		version:          "0.16.9",
		minecraftVersion: "1.21.4",
		packageDirs:      true,
	},
	"fabric-executable-262": {
		eco:              "fabric",
		version:          "0.19.3",
		minecraftVersion: "26.2",
		packageDirs:      true,
	},
	"fabric-installed-1214": {
		eco: "fabric", version: "0.16.9", minecraftVersion: "1.21.4",
	},
	"fabric-installed-262": {
		eco: "fabric", version: "0.19.3", minecraftVersion: "26.2",
	},
	"forge-1201": {
		eco:              "forge",
		version:          "47.4.10",
		minecraftVersion: "1.20.1",
		packageDirs:      true,
	},
	"forge-12111": {
		eco:              "forge",
		version:          "61.2.0",
		minecraftVersion: "1.21.11",
		packageDirs:      true,
	},
	"mcdr-fabric": {
		eco:               "fabric",
		version:           "0.16.9",
		minecraftVersion:  "1.21.4",
		packageDirs:       true,
		packageDirsSuffix: "/server/mods",
		packageDirsCheck:  "package_dirs.nested",
	},
	"mcdr-forge": {
		eco:               "forge",
		version:           "47.4.10",
		minecraftVersion:  "1.20.1",
		packageDirs:       true,
		packageDirsSuffix: "/server/mods",
		packageDirsCheck:  "package_dirs.nested",
	},
	"mcdr-neoforge": {
		eco:               "neoforge",
		version:           "20.2.93",
		minecraftVersion:  "1.20.2",
		packageDirs:       true,
		packageDirsSuffix: "/server/mods",
		packageDirsCheck:  "package_dirs.nested",
	},
	"neoforge-211": {
		eco:              "neoforge",
		version:          "21.1.248",
		minecraftVersion: "1.21.1",
		packageDirs:      true,
	},
	"neoforge-262": {
		eco:              "neoforge",
		version:          "26.2.0.67",
		minecraftVersion: "1.26.2",
		packageDirs:      true,
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
