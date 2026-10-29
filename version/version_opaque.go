package version

import (
	"fmt"
	"strings"
)

// parseOpaqueVersion accepts any non-empty identifier. Opaque versions have no
// ordering; they compare by exact string.
func parseOpaqueVersion(raw Version) ResolvableVersion {
	text := string(raw)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	return &StringVersion{
		dialect:  DialectOpaque,
		original: text,
	}
}

// parseOpaqueConstraint accepts an unconstrained expression or an exact
// identifier. Descriptors with an opaque version grammar define no range
// operators.
func parseOpaqueConstraint(raw string) (Expr, error) {
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
	version := parseOpaqueVersion(Version(text))
	if version == nil {
		return nil, fmt.Errorf("invalid opaque version %q", raw)
	}
	return Expr{
		{{Value: version, Operator: OpEq}},
	}, nil
}
