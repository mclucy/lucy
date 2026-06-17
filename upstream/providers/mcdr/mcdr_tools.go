package mcdr

import (
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/version"
)

func parseRequiredVersion(s string) (reqs []types.VersionSubExpr) {
	// MCDR metadata dependency requirements are AND criteria split by spaces.
	// References:
	//   - https://docs.mcdreforged.com/en/latest/plugin_dev/metadata.html
	//   - https://docs.npmjs.com/about-semantic-versioning
	expr := version.ParseRange(
		s,
		version.InferRangeDialect(types.PlatformMCDR),
		types.Semver,
	)
	if len(expr) == 0 {
		return nil
	}
	return expr[0]
}
