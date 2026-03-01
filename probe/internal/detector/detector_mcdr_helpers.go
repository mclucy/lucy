package detector

import (
	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/types"
)

// parseNpmVersionRange parses MCDR plugin dependency requirements.
//
// References:
//   - https://docs.mcdreforged.com/en/latest/plugin_dev/metadata.html
//   - https://docs.npmjs.com/about-semantic-versioning
//
// Note: call sites remain unchanged in detector; the parser implementation is
// centralized in the dependency package.
func parseNpmVersionRange(s string) types.VersionConstraintExpression {
	return dependency.ParseRange(
		s,
		dependency.InferRangeDialect(types.Mcdr),
		types.Semver,
	)
}
