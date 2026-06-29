package detector

import (
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/version"
)

// parseFabricVersionRanges parses a Fabric VersionRange value where each item
// in the outer slice is an OR alternative.
func parseFabricVersionRanges(
	ranges fn.SingleOrSlice[string],
) types.VersionExpr {
	return version.ParseRanges(
		[]string(ranges),
		version.InferRangeDialect(types.EcoFabric),
		types.Semver,
	)
}

// parseModLoaderMavenVersionRange parses Forge dependency version ranges.
//
// References:
//   - https://docs.minecraftforge.net/en/latest/gettingstarted/modfiles/
//   - https://maven.apache.org/enforcer/enforcer-rules/versionRanges.html
func parseModLoaderMavenVersionRange(interval string) [][]types.VersionSubExpr {
	return version.ParseRange(
		interval,
		version.InferRangeDialect(types.EcoForge),
		types.Maven,
	)
}

// parseNpmVersionRange parses MCDR plugin dependency requirements.
//
// References:
//   - https://docs.mcdreforged.com/en/latest/plugin_dev/metadata.html
//   - https://docs.npmjs.com/about-semantic-versioning
func parseNpmVersionRange(s string) types.VersionExpr {
	return version.ParseRange(
		s,
		version.InferRangeDialect(types.EcoMcdr),
		types.Semver,
	)
}
