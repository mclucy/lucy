package detector

import (
	"archive/zip"
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
)

var forgeRuntimeVersionDirPattern = regexp.MustCompile(
	`^(\d+\.\d+(?:\.\d+)?)-(\d+(?:\.\d+)+)$`,
)

var forgeJarNameVersionPattern = regexp.MustCompile(
	`^forge-(\d+\.\d+(?:\.\d+)?)-(\d+(?:\.\d+)+)(?:-[a-z]+)?\.jar$`,
)

func parseForgeManifest(
	zipReader *zip.Reader,
) (forgeVersion types.BareVersion, gameVersion types.BareVersion) {
	for _, f := range zipReader.File {
		if f.Name != "META-INF/MANIFEST.MF" {
			continue
		}

		r, err := f.Open()
		if err != nil {
			continue
		}
		defer fn.CloseReader(r, log.Warn)

		var inForgeSection bool
		var pendingImplVersion string
		s := bufio.NewScanner(r)
		for s.Scan() {
			line := s.Text()
			switch {
			case strings.HasPrefix(line, "Name: "):
				inForgeSection = false
			case line == "Implementation-Title: net.minecraftforge":
				inForgeSection = true
				if pendingImplVersion != "" {
					forgeVersion = types.BareVersion(pendingImplVersion)
					pendingImplVersion = ""
				}
			case strings.HasPrefix(line, "Implementation-Version: "):
				if after, found := strings.CutPrefix(
					line,
					"Implementation-Version: ",
				); found {
					switch {
					case inForgeSection:
						forgeVersion = types.BareVersion(after)
					case !inForgeSection && forgeVersion == "" && pendingImplVersion == "":
						pendingImplVersion = after
					}
				}
			case strings.HasPrefix(line, "Specification-Version: "):
				if after, found := strings.CutPrefix(
					line,
					"Specification-Version: ",
				); found && isMinecraftReleaseVersion(after) {
					gameVersion = types.BareVersion(after)
				}
			}
		}

		break
	}

	return forgeVersion, gameVersion
}

func isMinecraftReleaseVersion(version string) bool {
	if !strings.HasPrefix(version, "1.") {
		return false
	}
	for _, r := range version {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}

func parseForgeVersionTupleFromPath(
	filePath string,
) (gameVersion types.BareVersion, forgeVersion types.BareVersion, ok bool) {
	parts := strings.Split(filepath.ToSlash(filePath), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] != "forge" {
			continue
		}
		match := forgeRuntimeVersionDirPattern.FindStringSubmatch(parts[i+1])
		if match == nil {
			continue
		}
		return types.BareVersion(match[1]), types.BareVersion(match[2]), true
	}
	if match := forgeJarNameVersionPattern.FindStringSubmatch(filepath.Base(filePath)); match != nil {
		return types.BareVersion(match[1]), types.BareVersion(match[2]), true
	}
	return types.VersionUnknown, types.VersionUnknown, false
}

func hasConcreteVersion(version types.BareVersion) bool {
	return version != "" && !version.IsInvalid() && !version.CanInfer()
}

func compareForgeMajor(version types.BareVersion, target int) int {
	major := strings.Split(string(version), ".")[0]
	if major == "" {
		return -1
	}
	n, err := strconv.Atoi(major)
	if err != nil {
		return -1
	}
	switch {
	case n < target:
		return -1
	case n > target:
		return 1
	default:
		return 0
	}
}

func forgeHasSibling(filePath string, siblings ...string) bool {
	dir := filepath.Dir(filePath)
	for _, sibling := range siblings {
		if _, err := os.Stat(filepath.Join(dir, sibling)); err == nil {
			return true
		}
	}
	return false
}

func buildForgeRuntimeInfo(
	filePath string,
	gameVersion types.BareVersion,
	forgeVersion types.BareVersion,
) *ExecutableEvidence {
	return &ExecutableEvidence{
		PrimaryEntrance: filePath,
		GameVersion:     gameVersion,
		RuntimeIdentities: []types.VersionedPackageRef{
			{
				PackageRef: types.PackageRef{
					Platform: types.PlatformForge,
					Name:     "forge",
				},
				Version: forgeVersion,
			},
			{
				PackageRef: types.PackageRef{
					Platform: types.PlatformMinecraft,
					Name:     "minecraft",
				},
				Version: gameVersion,
			},
		},
		Topology: &types.RuntimeTopology{
			PrimaryNode: "forge",
			Nodes: []types.RuntimeNode{
				{
					ID:           "forge",
					Role:         types.RuntimeRoleModLoader,
					Capabilities: []types.RuntimeCapability{types.CapabilityForgeMods},
				},
			},
		},
	}
}
