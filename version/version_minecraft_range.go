package version

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/types"
)

// parseMinecraftConstraint accepts exact release or snapshot versions as OR
// alternatives. Minecraft metadata defines no range operators.
func parseMinecraftConstraint(raw string) (types.VersionExpr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" {
		return nil, nil
	}

	parts := strings.Split(raw, "||")
	result := make(types.VersionExpr, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty alternative in Minecraft range %q", raw)
		}
		if isWildcardExpression(part) {
			return nil, nil
		}
		version := parseMinecraftVersion(types.Version(part))
		if version == nil {
			return nil, fmt.Errorf("invalid Minecraft version %q", part)
		}
		result = append(result, []types.VersionSubExpr{
			{Value: version, Operator: types.OpEq},
		})
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}
