package detector

import (
	"archive/zip"
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
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
		defer tools.CloseReader(r, logger.Warn)

		s := bufio.NewScanner(r)
		for s.Scan() {
			line := s.Text()
			if line == "Implementation-Title: net.minecraftforge" {
				if !s.Scan() {
					continue
				}
				if after, found := strings.CutPrefix(
					s.Text(),
					"Implementation-Version: ",
				); found {
					forgeVersion = types.BareVersion(after)
				}
			}
			if strings.HasPrefix(line, "Specification-Version: ") {
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
	switch major {
	case "":
		return -1
	case "61":
		if target == 61 {
			return 0
		}
		if target < 61 {
			return 1
		}
		return -1
	default:
		if major > "61" {
			if target <= 61 {
				return 1
			}
			return -1
		}
		return -1
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
				Platform: types.PlatformForge,
				Name:     "forge",
				Version:  forgeVersion,
			},
			{
				Platform: types.PlatformMinecraft,
				Name:     "minecraft",
				Version:  gameVersion,
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
