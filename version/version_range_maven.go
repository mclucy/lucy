package version

import (
	"fmt"
	"strings"
)

// parseMavenRange parses Maven version ranges used by Forge and NeoForge.
// A bare version is a recommended version, not a restriction.
//
// References:
//   - https://docs.minecraftforge.net/en/latest/gettingstarted/modfiles/
//   - https://maven.apache.org/enforcer/enforcer-rules/versionRanges.html
func parseMavenRange(raw string) (Expr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" || strings.EqualFold(raw, "none") {
		return nil, nil
	}

	parts, err := splitMavenUnions(raw)
	if err != nil {
		return nil, err
	}

	result := make(Expr, 0, len(parts))
	for _, part := range parts {
		constraints, err := parseMavenSingleRange(strings.TrimSpace(part))
		if err != nil {
			return nil, err
		}
		if len(constraints) == 0 {
			continue
		}
		result = append(result, constraints)
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func splitMavenUnions(raw string) ([]string, error) {
	var out []string
	depth := 0
	start := 0
	flush := func(end int) error {
		part := strings.TrimSpace(raw[start:end])
		if part == "" {
			return fmt.Errorf("invalid Maven range %q", raw)
		}
		out = append(out, part)
		return nil
	}
	for index := range len(raw) {
		switch raw[index] {
		case '[', '(':
			depth++
		case ']', ')':
			if depth == 0 {
				return nil, fmt.Errorf("invalid Maven range %q", raw)
			}
			depth--
		case ',':
			if depth == 0 {
				if err := flush(index); err != nil {
					return nil, err
				}
				start = index + 1
			}
		}
	}
	if depth != 0 {
		return nil, fmt.Errorf("invalid Maven range %q", raw)
	}
	if err := flush(len(raw)); err != nil {
		return nil, err
	}
	return out, nil
}

func parseMavenSingleRange(raw string) ([]SubExpr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty Maven range")
	}

	if !startsRange(raw) {
		if parseMavenVersion(Version(raw)) == nil {
			return nil, fmt.Errorf("invalid Maven version %q", raw)
		}
		return nil, nil
	}

	if !endsRange(raw) {
		return nil, fmt.Errorf("invalid Maven range %q", raw)
	}
	left := raw[0]
	right := raw[len(raw)-1]
	if !((left == '[' || left == '(') && (right == ']' || right == ')')) {
		return nil, fmt.Errorf("invalid Maven range %q", raw)
	}

	body := strings.TrimSpace(raw[1 : len(raw)-1])
	if !strings.Contains(body, ",") {
		if left == '[' && right == ']' && body != "" {
			version := parseMavenVersion(Version(body))
			if version == nil {
				return nil, fmt.Errorf("invalid Maven version %q", body)
			}
			return []SubExpr{
				{Value: version, Operator: OpEq},
			}, nil
		}
		return nil, fmt.Errorf("invalid Maven range %q", raw)
	}

	bounds := strings.SplitN(body, ",", 2)
	lowerToken := strings.TrimSpace(bounds[0])
	upperToken := strings.TrimSpace(bounds[1])
	var lower, upper *MavenVersion
	out := make([]SubExpr, 0, 2)
	if lowerToken != "" {
		parsed := parseMavenVersion(Version(lowerToken))
		if parsed == nil {
			return nil, fmt.Errorf("invalid Maven version %q", lowerToken)
		}
		lower = parsed.(*MavenVersion)
		operator := OpGt
		if left == '[' {
			operator = OpGte
		}
		out = append(out, SubExpr{Value: lower, Operator: operator})
	}
	if upperToken != "" {
		parsed := parseMavenVersion(Version(upperToken))
		if parsed == nil {
			return nil, fmt.Errorf("invalid Maven version %q", upperToken)
		}
		upper = parsed.(*MavenVersion)
		operator := OpLt
		if right == ']' {
			operator = OpLte
		}
		out = append(out, SubExpr{Value: upper, Operator: operator})
	}
	if lower != nil && upper != nil {
		comparison, comparable := lower.Compare(upper)
		if !comparable {
			return nil, fmt.Errorf("incomparable Maven bounds in %q", raw)
		}
		if comparison > 0 {
			return nil, fmt.Errorf("descending Maven range %q", raw)
		}
		if comparison == 0 && (left == '(' || right == ')') {
			return nil, fmt.Errorf("empty Maven range %q", raw)
		}
	}
	return out, nil
}

func startsRange(raw string) bool {
	return strings.HasPrefix(raw, "[") || strings.HasPrefix(raw, "(")
}

func endsRange(raw string) bool {
	return strings.HasSuffix(raw, "]") || strings.HasSuffix(raw, ")")
}
