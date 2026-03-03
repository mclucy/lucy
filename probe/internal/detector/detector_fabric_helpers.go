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
) types.VersionConstraintExpression {
	return dependency.ParseRanges(
		[]string(ranges),
		dependency.InferRangeDialect(types.PlatformFabric),
		types.Semver,
	)
}
