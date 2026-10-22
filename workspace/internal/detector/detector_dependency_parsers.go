package detector

import (
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/version"
)

// parseFabricVersionRanges parses a Fabric VersionRange value where each item
// in the outer slice is an OR alternative.
func parseFabricVersionRanges(
	ranges fn.SingleOrSlice[string],
) (version.Expr, error) {
	return version.ParseConstraints(ranges, version.DialectFabric)
}

// parseModLoaderMavenVersionRange parses Forge dependency version ranges.
//
// References:
//   - https://docs.minecraftforge.net/en/latest/gettingstarted/modfiles/
//   - https://maven.apache.org/enforcer/enforcer-rules/versionRanges.html
func parseModLoaderMavenVersionRange(
	interval string,
) (version.Expr, error) {
	return version.ParseConstraint(interval, version.DialectMaven)
}

// parseNpmVersionRange parses MCDR plugin dependency requirements.
//
// References:
//   - https://docs.mcdreforged.com/en/latest/plugin_dev/metadata.html
//   - https://docs.npmjs.com/about-semantic-versioning
func parseNpmVersionRange(s string) (version.Expr, error) {
	return version.ParseConstraint(s, version.DialectMCDR)
}
