package mcdr

import (
	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/types"
)

func parseRequiredVersion(s string) (reqs []types.VersionConstraint) {
	// MCDR metadata dependency requirements are AND criteria split by spaces.
	// References:
	//   - https://docs.mcdreforged.com/en/latest/plugin_dev/metadata.html
	//   - https://docs.npmjs.com/about-semantic-versioning
	expr := dependency.ParseRangeByPlatform(s, types.Mcdr, types.Semver)
	if len(expr) == 0 {
		return nil
	}
	return expr[0]
}
