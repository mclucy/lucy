package discovery

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
)

var neoforgeVersionDirPattern = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.\d+)*(?:-[a-zA-Z0-9.]+)*$`)

// parseModLoaderVersionTuple derives the Minecraft and loader versions from a
// Maven version directory name. NeoForge used to drop the Minecraft "1."
// prefix (20.2.x -> 1.20.2, 21.1.x -> 1.21.1); the year scheme keeps the
// Minecraft triple and appends a build (26.2.0.67 -> 26.2). Only the
// directory name is considered; content evidence refines the result where
// the loader publishes it.
func parseModLoaderVersionTuple(
	versionDir string,
	platform catalog.Ecosystem,
) (gameVersion, loaderVersion versionpkg.Version, ok bool) {
	name := filepath.Base(versionDir)

	switch platform {
	case catalog.EcosystemForge:
		match := forgeRuntimeVersionDirPattern.FindStringSubmatch(name)
		if match == nil {
			return versionpkg.Unknown, versionpkg.Unknown, false
		}
		return versionpkg.Version(match[1]), versionpkg.Version(match[2]), true
	case catalog.EcosystemNeoForge:
		if neoforgeVersionDirPattern.FindStringSubmatch(name) == nil {
			return versionpkg.Unknown, versionpkg.Unknown, false
		}
		gameVersion = neoforgeGameVersionFromTuple(name)
		if gameVersion == versionpkg.Unknown {
			return versionpkg.Unknown, versionpkg.Unknown, false
		}
		return gameVersion, versionpkg.Version(name), true
	default:
		return versionpkg.Unknown, versionpkg.Unknown, false
	}
}

// neoforgeGameVersionFromTuple maps a NeoForge loader version to the
// Minecraft release it targets. Classic lines drop the leading 1; year-scheme
// lines keep the Minecraft triple and add a build segment, where a zero patch
// is not part of the release id (26.2.0.67 -> 26.2, 26.2.1.5 -> 26.2.1).
func neoforgeGameVersionFromTuple(name string) versionpkg.Version {
	segments := strings.Split(strings.SplitN(name, "-", 2)[0], ".")
	if len(segments) < 2 {
		return versionpkg.Unknown
	}
	major, err := strconv.Atoi(segments[0])
	if err != nil {
		return versionpkg.Unknown
	}
	if major < 26 {
		return versionpkg.Version("1." + segments[0] + "." + segments[1])
	}
	gameVersion := segments[0] + "." + segments[1]
	if len(segments) >= 4 && segments[2] != "0" {
		gameVersion += "." + segments[2]
	}
	return versionpkg.Version(gameVersion)
}
