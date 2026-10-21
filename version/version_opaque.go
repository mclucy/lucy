package version

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/types"
)

// parseOpaqueVersion accepts any non-empty identifier. Opaque versions have no
// ordering; they compare by exact string.
func parseOpaqueVersion(raw types.Version) types.ResolvableVersion {
	text := string(raw)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return &StringVersion{
		dialect:  types.VersionDialectOpaque,
		original: text,
	}
}

// parseOpaqueConstraint accepts an unconstrained expression or an exact
// identifier. Descriptors with an opaque version grammar define no range
// operators.
func parseOpaqueConstraint(raw string) (types.VersionExpr, error) {
	text := strings.TrimSpace(raw)
	if text == "" || text == "*" {
		return nil, nil
	}
	if strings.ContainsAny(text, " \t\r\n|") {
		return nil, fmt.Errorf("opaque constraint %q must be an exact version", raw)
	}
	switch text[0] {
	case '<', '>', '=', '!', '^', '~':
		return nil, fmt.Errorf("opaque constraint %q must be an exact version", raw)
	}
	version := parseOpaqueVersion(types.Version(text))
	if version == nil {
		return nil, fmt.Errorf("invalid opaque version %q", raw)
	}
	return types.VersionExpr{
		{{Value: version, Operator: types.OpEq}},
	}, nil
}
