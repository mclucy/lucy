package detector

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mclucy/lucy/dependency"
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

// getForgeModVersion extracts the version from a Forge JAR's manifest
// when the mod version is set to `${file.jarVersion}`
func getForgeModVersion(zip *zip.Reader) types.RawVersion {
	var r io.ReadCloser
	var err error
	for _, f := range zip.File {
		if f.Name == "META-INF/MANIFEST.MF" {
			r, err = f.Open()
			if err != nil {
				return types.VersionUnknown
			}
			defer tools.CloseReader(r, logger.Warn)
			break
		}
	}

	if r == nil {
		return types.VersionUnknown
	}

	data, err := io.ReadAll(r)
	if err != nil {
		return types.VersionUnknown
	}
	manifest := string(data)
	const versionField = "Implementation-Version: "
	idx := strings.Index(manifest, versionField)
	if idx == -1 {
		return types.VersionUnknown
	}
	i := idx + len(versionField)
	v := manifest[i:]
	v = strings.Split(v, "\r")[0]
	v = strings.Split(v, "\n")[0]
	return types.RawVersion(v)
}

// parseModLoaderMavenVersionRange parses Forge dependency version ranges.
//
// References:
//   - https://docs.minecraftforge.net/en/latest/gettingstarted/modfiles/
//   - https://maven.apache.org/enforcer/enforcer-rules/versionRanges.html
func parseModLoaderMavenVersionRange(interval string) [][]types.VersionConstraint {
	return dependency.ParseRange(
		interval,
		dependency.InferRangeDialect(types.PlatformForge),
		types.Semver,
	)
}

func parseForgeVersionTupleFromPath(
	filePath string,
) (gameVersion types.RawVersion, forgeVersion types.RawVersion, ok bool) {
	parts := strings.Split(filepath.ToSlash(filePath), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] != "forge" {
			continue
		}
		match := forgeRuntimeVersionDirPattern.FindStringSubmatch(parts[i+1])
		if match == nil {
			continue
		}
		return types.RawVersion(match[1]), types.RawVersion(match[2]), true
	}
	if match := forgeJarNameVersionPattern.FindStringSubmatch(filepath.Base(filePath)); match != nil {
		return types.RawVersion(match[1]), types.RawVersion(match[2]), true
	}
	return types.VersionUnknown, types.VersionUnknown, false
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

func hasConcreteVersion(version types.RawVersion) bool {
	return version != "" && !version.IsInvalid() && !version.CanInfer()
}

func buildForgeRuntimeInfo(
	filePath string,
	gameVersion types.RawVersion,
	forgeVersion types.RawVersion,
) *types.RuntimeInfo {
	return &types.RuntimeInfo{
		PrimaryEntrance: filePath,
		GameVersion:     gameVersion,
		BootCommand:     nil,
		RuntimeIdentities: []types.PackageId{
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
					ID:               "forge",
					Role:             types.RuntimeRoleModLoader,
					IdentityPlatform: types.PlatformForge,
					Capabilities:     []types.RuntimeCapability{types.CapabilityForgeMods},
				},
			},
		},
	}
}
