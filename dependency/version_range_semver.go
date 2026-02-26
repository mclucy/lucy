package dependency

import (
	"strconv"
	"strings"

	"github.com/mclucy/lucy/types"
)

type caretMode uint8

const (
	caretModeNpm caretMode = iota
	caretModeSameMajor
)

type semverRangeOptions struct {
	caretMode caretMode
}

// parseMcdrSemverRange parses MCDR dependency range expressions strictly by
// metadata docs: space-separated criteria (AND) with operators
// >=, >, <=, <, =, ==, ^, ~ and wildcard base versions.
// Reference: https://docs.mcdreforged.com/en/latest/plugin_dev/metadata.html
func parseMcdrSemverRange(raw string) types.VersionConstraintExpression {
	raw = strings.TrimSpace(raw)
	if raw == "" || isWildcardToken(raw) {
		return nil
	}

	tokens := strings.Fields(raw)
	if len(tokens) == 0 {
		return nil
	}

	andConstraints := make([]types.VersionConstraint, 0, len(tokens))
	for _, token := range tokens {
		constraints, ok := parseMcdrSemverCriterion(token)
		if !ok {
			return nil
		}
		andConstraints = append(andConstraints, constraints...)
	}

	if len(andConstraints) == 0 {
		return nil
	}
	return types.VersionConstraintExpression{andConstraints}
}

func parseMcdrSemverCriterion(raw string) ([]types.VersionConstraint, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || isWildcardToken(raw) {
		return nil, true
	}

	operator := ""
	versionToken := raw
	for _, op := range []string{">=", "<=", "==", ">", "<", "=", "^", "~"} {
		if strings.HasPrefix(raw, op) {
			operator = op
			versionToken = strings.TrimSpace(strings.TrimPrefix(raw, op))
			break
		}
	}
	if versionToken == "" {
		return nil, false
	}

	if strings.ContainsAny(versionToken, "xX*") {
		switch operator {
		case "", "=", "==":
			constraints := parseXRange(versionToken)
			if constraints == nil {
				return nil, false
			}
			return constraints, true
		default:
			// Keep implementation strict to documented examples.
			return nil, false
		}
	}

	lower := parseSemver(types.RawVersion(versionToken))
	if lower == nil {
		return nil, false
	}

	switch operator {
	case "", "=", "==":
		return []types.VersionConstraint{
			{
				Value: lower, Operator: types.OpEq,
			},
		}, true
	case ">":
		return []types.VersionConstraint{
			{
				Value: lower, Operator: types.OpGt,
			},
		}, true
	case ">=":
		return []types.VersionConstraint{
			{
				Value: lower, Operator: types.OpGte,
			},
		}, true
	case "<":
		return []types.VersionConstraint{
			{
				Value: lower, Operator: types.OpLt,
			},
		}, true
	case "<=":
		return []types.VersionConstraint{
			{
				Value: lower, Operator: types.OpLte,
			},
		}, true
	case "^":
		constraints := parseCaretRangeFromSemver(lower, caretModeSameMajor)
		return constraints, len(constraints) > 0
	case "~":
		constraints := parseTildeRangeFromSemver(lower, versionToken)
		return constraints, len(constraints) > 0
	default:
		return nil, false
	}
}

func parseSemverRange(
	raw string,
	options semverRangeOptions,
) types.VersionConstraintExpression {
	raw = strings.TrimSpace(raw)
	if raw == "" || isWildcardToken(raw) {
		return nil
	}

	orParts := strings.Split(raw, "||")
	result := make(types.VersionConstraintExpression, 0, len(orParts))

	for _, part := range orParts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, " - ") {
			rangeConstraints := parseSemverHyphenRange(part)
			if len(rangeConstraints) == 0 {
				continue
			}
			result = append(result, rangeConstraints)
			continue
		}

		tokens := strings.Fields(part)
		if len(tokens) == 0 {
			continue
		}

		andConstraints := make([]types.VersionConstraint, 0, len(tokens))
		valid := true
		for _, token := range tokens {
			tokenConstraints, ok := parseSemverToken(token, options)
			if !ok {
				valid = false
				break
			}
			andConstraints = append(andConstraints, tokenConstraints...)
		}
		if !valid {
			continue
		}

		if len(andConstraints) == 0 {
			// all wildcard/no-op constraints => no constraint for the whole range
			return nil
		}
		result = append(result, andConstraints)
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func parseSemverHyphenRange(raw string) []types.VersionConstraint {
	tokens := strings.SplitN(raw, " - ", 2)
	if len(tokens) != 2 {
		return nil
	}
	left := parseSemver(types.RawVersion(strings.TrimSpace(tokens[0])))
	right := parseSemver(types.RawVersion(strings.TrimSpace(tokens[1])))
	if left == nil || right == nil {
		return nil
	}
	return []types.VersionConstraint{
		{Value: left, Operator: types.OpGte},
		{Value: right, Operator: types.OpLte},
	}
}

func parseSemverToken(
	raw string,
	options semverRangeOptions,
) ([]types.VersionConstraint, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || isWildcardToken(raw) {
		return nil, true
	}

	// comma-separated comparators within one token are treated as AND
	if strings.Contains(raw, ",") {
		parts := strings.Split(raw, ",")
		all := make([]types.VersionConstraint, 0, len(parts))
		for _, part := range parts {
			constraints, ok := parseSemverToken(part, options)
			if !ok {
				return nil, false
			}
			all = append(all, constraints...)
		}
		return all, true
	}

	for _, op := range []string{
		">=", "<=", "!=", "==", ">", "<", "=", "^", "~",
	} {
		if strings.HasPrefix(raw, op) {
			versionToken := strings.TrimSpace(strings.TrimPrefix(raw, op))
			if versionToken == "" {
				return nil, false
			}
			return parseSemverOperator(op, versionToken, options)
		}
	}

	if strings.ContainsAny(raw, "xX*") {
		constraints := parseXRange(raw)
		if constraints == nil && !isWildcardToken(raw) {
			return nil, false
		}
		return constraints, true
	}

	v := parseSemver(types.RawVersion(raw))
	if v == nil {
		return nil, false
	}
	return []types.VersionConstraint{{Value: v, Operator: types.OpEq}}, true
}

func parseSemverOperator(
	op string,
	versionToken string,
	options semverRangeOptions,
) ([]types.VersionConstraint, bool) {
	if strings.ContainsAny(versionToken, "xX*") {
		switch op {
		case "=":
			constraints := parseXRange(versionToken)
			if constraints == nil {
				return nil, false
			}
			return constraints, true
		case "!=":
			// This desugars to an OR expression and is intentionally unsupported in
			// the single-token operator parser.
			return nil, false
		}
	}

	lower := parseSemver(types.RawVersion(versionToken))
	if lower == nil && strings.ContainsAny(versionToken, "xX*") {
		lower = parseSemverLowerBoundFromWildcard(versionToken)
	}
	if lower == nil {
		return nil, false
	}

	switch op {
	case "==":
		return []types.VersionConstraint{
			{
				Value: lower, Operator: types.OpEq,
			},
		}, true
	case "=":
		return []types.VersionConstraint{
			{
				Value: lower, Operator: types.OpEq,
			},
		}, true
	case "!=":
		return []types.VersionConstraint{
			{
				Value: lower, Operator: types.OpNeq,
			},
		}, true
	case ">":
		return []types.VersionConstraint{
			{
				Value: lower, Operator: types.OpGt,
			},
		}, true
	case ">=":
		return []types.VersionConstraint{
			{
				Value: lower, Operator: types.OpGte,
			},
		}, true
	case "<":
		return []types.VersionConstraint{
			{
				Value: lower, Operator: types.OpLt,
			},
		}, true
	case "<=":
		return []types.VersionConstraint{
			{
				Value: lower, Operator: types.OpLte,
			},
		}, true
	case "^":
		return parseCaretRangeFromSemver(lower, options.caretMode), true
	case "~":
		return parseTildeRangeFromSemver(lower, versionToken), true
	default:
		return nil, false
	}
}

func parseCaretRangeFromSemver(
	lower types.ComparableVersion,
	mode caretMode,
) []types.VersionConstraint {
	sv, ok := lower.(*SemverVersion)
	if !ok {
		return nil
	}

	major := sv.Major()
	minor := sv.Minor()
	patch := sv.Patch()

	var upper types.ComparableVersion
	if mode == caretModeNpm {
		if major > 0 {
			upper = NewSemver(major+1, 0, 0)
		} else if minor > 0 {
			upper = NewSemver(0, minor+1, 0)
		} else {
			upper = NewSemver(0, 0, patch+1)
		}
	} else {
		upper = NewSemver(major+1, 0, 0)
	}

	if upper == nil {
		return nil
	}

	return []types.VersionConstraint{
		{Value: lower, Operator: types.OpGte},
		{Value: upper, Operator: types.OpLt},
	}
}

func parseTildeRangeFromSemver(
	lower types.ComparableVersion,
	raw string,
) []types.VersionConstraint {
	sv, ok := lower.(*SemverVersion)
	if !ok {
		return nil
	}

	parts := strings.Split(strings.TrimSpace(raw), ".")
	hasMinorSpecified := len(parts) >= 2 && !isWildcardToken(parts[1])

	var upper types.ComparableVersion
	if !hasMinorSpecified {
		upper = NewSemver(sv.Major()+1, 0, 0)
	} else {
		upper = NewSemver(sv.Major(), sv.Minor()+1, 0)
	}
	if upper == nil {
		return nil
	}

	return []types.VersionConstraint{
		{Value: lower, Operator: types.OpGte},
		{Value: upper, Operator: types.OpLt},
	}
}

func parseXRange(raw string) []types.VersionConstraint {
	raw = strings.TrimSpace(raw)
	if isWildcardToken(raw) {
		return nil
	}

	parts := strings.Split(raw, ".")
	if len(parts) == 0 {
		return nil
	}

	if len(parts) >= 1 && isWildcardToken(parts[0]) {
		return nil
	}

	major, ok := parseUint64(parts[0])
	if !ok {
		return nil
	}

	if len(parts) == 1 {
		return nil
	}

	if len(parts) >= 2 && isWildcardToken(parts[1]) {
		lower := NewSemver(major, 0, 0)
		upper := NewSemver(major+1, 0, 0)
		if lower == nil || upper == nil {
			return nil
		}
		return []types.VersionConstraint{
			{Value: lower, Operator: types.OpGte},
			{Value: upper, Operator: types.OpLt},
		}
	}

	minor, ok := parseUint64(parts[1])
	if !ok {
		return nil
	}
	if len(parts) >= 3 && isWildcardToken(parts[2]) {
		lower := NewSemver(major, minor, 0)
		upper := NewSemver(major, minor+1, 0)
		if lower == nil || upper == nil {
			return nil
		}
		return []types.VersionConstraint{
			{Value: lower, Operator: types.OpGte},
			{Value: upper, Operator: types.OpLt},
		}
	}

	return nil
}

func parseSemverLowerBoundFromWildcard(raw string) types.ComparableVersion {
	parts := strings.Split(strings.TrimSpace(raw), ".")
	if len(parts) == 0 {
		return nil
	}
	for i := range parts {
		if isWildcardToken(parts[i]) {
			parts[i] = "0"
		}
	}
	for len(parts) < 3 {
		parts = append(parts, "0")
	}
	if len(parts) > 3 {
		parts = parts[:3]
	}
	return parseSemver(types.RawVersion(strings.Join(parts, ".")))
}

func isWildcardToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return true
	}
	return token == "*" || token == "x" || token == "X"
}

func parseUint64(token string) (uint64, bool) {
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, false
	}
	v, err := strconv.ParseUint(token, 10, 64)
	if err != nil {
		return 0, false
	}
	return v, true
}
