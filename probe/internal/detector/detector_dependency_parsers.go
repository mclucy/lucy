package detector

import (
	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
)

// parseFabricVersionRanges parses a Fabric VersionRange value where each item
// in the outer slice is an OR alternative.
func parseFabricVersionRanges(
	ranges tools.SingleOrSlice[string],
) types.VersionExpr {
	return dependency.ParseRanges(
		[]string(ranges),
		dependency.InferRangeDialect(types.PlatformFabric),
		types.Semver,
	)
}

// parseModLoaderMavenVersionRange parses Forge dependency version ranges.
//
// References:
//   - https://docs.minecraftforge.net/en/latest/gettingstarted/modfiles/
//   - https://maven.apache.org/enforcer/enforcer-rules/versionRanges.html
func parseModLoaderMavenVersionRange(interval string) [][]types.VersionSubExpr {
	return dependency.ParseRange(
		interval,
		dependency.InferRangeDialect(types.PlatformForge),
		types.Maven,
	)
}

// parseNpmVersionRange parses MCDR plugin dependency requirements.
//
// References:
//   - https://docs.mcdreforged.com/en/latest/plugin_dev/metadata.html
//   - https://docs.npmjs.com/about-semantic-versioning
func parseNpmVersionRange(s string) types.VersionExpr {
	return dependency.ParseRange(
		s,
		dependency.InferRangeDialect(types.PlatformMCDR),
		types.Semver,
	)
}
