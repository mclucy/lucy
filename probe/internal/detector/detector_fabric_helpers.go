package detector

import (
	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/types"
)

// parseFabricVersionRange parses Fabric dependency requirements.
//
// Reference: https://wiki.fabricmc.net/documentation:fabric_mod_json_spec
func parseFabricVersionRange(s string) types.VersionConstraintExpression {
	return dependency.ParseRangeByPlatform(s, types.Fabric, types.Semver)
}
