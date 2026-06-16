package input

import (
	"fmt"

	"github.com/mclucy/lucy/types"
)

// ParsePackageRequest parses a package specifier string with source scope
// into a structured PackageRequest suitable for installation.
func ParsePackageRequest(s string, sourceHint string) (types.ScopedPackageRef, types.BareVersion, error) {
	id, err := Parse(s)
	if err != nil {
		return types.ScopedPackageRef{}, types.VersionNone, err
	}

	scope := types.SourceAuto
	if sourceHint != "" {
		parsedScope := types.ParseSource(sourceHint)
		if parsedScope == types.SourceUnknown {
			return types.ScopedPackageRef{}, types.VersionNone, fmt.Errorf("invalid source: %s", sourceHint)
		}
		scope = parsedScope
	}

	scoped := types.ScopedPackageRef{
		PackageRef: id.PackageRef,
		Scope:      scope,
	}

	return scoped, id.Version, nil
}
