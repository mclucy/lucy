package dependency

import (
	"strings"

	"github.com/mclucy/lucy/types"
)

func parseMavenRange(raw string) types.VersionConstraintExpression {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" || strings.EqualFold(raw, "none") {
		return nil
	}

	parts := splitMavenUnions(raw)
	if len(parts) == 0 {
		parts = []string{raw}
	}

	result := make(types.VersionConstraintExpression, 0, len(parts))
	for _, part := range parts {
		constraints := parseMavenSingleRange(strings.TrimSpace(part))
		if len(constraints) == 0 {
			continue
		}
		result = append(result, constraints)
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func splitMavenUnions(raw string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(raw); i++ {
		switch raw[i] {
		case '[', '(':
			depth++
		case ']', ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				part := strings.TrimSpace(raw[start:i])
				if part != "" {
					out = append(out, part)
				}
				start = i + 1
			}
		}
	}
	if start < len(raw) {
		part := strings.TrimSpace(raw[start:])
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func parseMavenSingleRange(raw string) []types.VersionConstraint {
	if raw == "" {
		return nil
	}

	if strings.HasPrefix(raw, "^") || strings.HasPrefix(raw, "~") {
		// Not part of Maven version range syntax.
		// Forge docs (1.21.x, checked 2026-02-24) point dependency versionRange
		// to Maven Version Range syntax, which only defines bracket/parenthesis
		// ranges and basic comparison operators.
		// References:
		//   - https://docs.minecraftforge.net/en/latest/gettingstarted/modfiles/
		//   - https://maven.apache.org/enforcer/enforcer-rules/versionRanges.html
		return nil
	}

	if len(raw) >= 2 {
		left := raw[0]
		right := raw[len(raw)-1]
		if (left == '[' || left == '(') && (right == ']' || right == ')') {
			body := strings.TrimSpace(raw[1 : len(raw)-1])
			if strings.Contains(body, ",") {
				bounds := strings.SplitN(body, ",", 2)
				lowerToken := strings.TrimSpace(bounds[0])
				upperToken := strings.TrimSpace(bounds[1])
				out := make([]types.VersionConstraint, 0, 2)
				if lowerToken != "" {
					lower := parseSemver(types.RawVersion(lowerToken))
					if lower == nil {
						return nil
					}
					op := types.OpGt
					if left == '[' {
						op = types.OpGte
					}
					out = append(
						out,
						types.VersionConstraint{Value: lower, Operator: op},
					)
				}
				if upperToken != "" {
					upper := parseSemver(types.RawVersion(upperToken))
					if upper == nil {
						return nil
					}
					op := types.OpLt
					if right == ']' {
						op = types.OpLte
					}
					out = append(
						out,
						types.VersionConstraint{Value: upper, Operator: op},
					)
				}
				if len(out) == 0 {
					return nil
				}
				return out
			}

			// Exact value form: [1.0]
			if left == '[' && right == ']' && body != "" {
				v := parseSemver(types.RawVersion(body))
				if v == nil {
					return nil
				}
				return []types.VersionConstraint{
					{
						Value: v, Operator: types.OpEq,
					},
				}
			}
			return nil
		}
	}

	operator := types.OpEq
	versionToken := raw
	for _, op := range []struct {
		prefix   string
		operator types.VersionOperator
	}{
		{prefix: ">=", operator: types.OpGte},
		{prefix: "<=", operator: types.OpLte},
		{prefix: "!=", operator: types.OpNeq},
		{prefix: ">", operator: types.OpGt},
		{prefix: "<", operator: types.OpLt},
		{prefix: "=", operator: types.OpEq},
	} {
		if strings.HasPrefix(raw, op.prefix) {
			operator = op.operator
			versionToken = strings.TrimSpace(strings.TrimPrefix(raw, op.prefix))
			break
		}
	}
	v := parseSemver(types.RawVersion(versionToken))
	if v == nil {
		return nil
	}
	return []types.VersionConstraint{{Value: v, Operator: operator}}
}
