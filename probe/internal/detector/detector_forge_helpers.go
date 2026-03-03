package detector

import (
	"archive/zip"
	"io"
	"strings"

	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
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

// parseMavenVersionRange parses Forge dependency version ranges.
//
// References:
//   - https://docs.minecraftforge.net/en/latest/gettingstarted/modfiles/
//   - https://maven.apache.org/enforcer/enforcer-rules/versionRanges.html
func parseMavenVersionRange(interval string) [][]types.VersionConstraint {
	return dependency.ParseRange(
		interval,
		dependency.InferRangeDialect(types.Forge),
		types.Semver,
	)
}
