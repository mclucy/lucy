package version

import (
	"fmt"
	"strconv"
	"strings"
)

// parseFabricConstraint parses fabric.mod.json dependency predicates. Each
// entry of a VersionRange array is an alternative, which ParseConstraints
// merges into OR clauses.
//
// Reference: https://wiki.fabricmc.net/documentation:fabric_mod_json_spec
func parseFabricConstraint(raw string) (Expr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || isWildcardExpression(raw) {
		return nil, nil
	}

	parts := strings.Split(raw, "||")
	result := make(Expr, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty alternative in Fabric range %q", raw)
		}
		terms, err := parseFabricClause(part)
		if err != nil {
			return nil, err
		}
		if len(terms) == 0 {
			return nil, nil
		}
		result = append(result, terms)
	}
	if len(result) == 0 {
		return nil, nil
	}
	return result, nil
}

func parseFabricClause(raw string) ([]SubExpr, error) {
	if strings.Contains(raw, " - ") {
		return parseFabricHyphenRange(raw)
	}

	tokens := strings.Fields(raw)
	terms := make([]SubExpr, 0, len(tokens))
	for _, token := range tokens {
		parsed, err := parseFabricToken(token)
		if err != nil {
			return nil, err
		}
		terms = append(terms, parsed...)
	}
	return terms, nil
}

func parseFabricHyphenRange(raw string) ([]SubExpr, error) {
	tokens := strings.SplitN(raw, " - ", 2)
	if len(tokens) != 2 {
		return nil, fmt.Errorf("invalid Fabric range %q", raw)
	}
	lower := parseFabricVersion(Version(strings.TrimSpace(tokens[0])))
	upper := parseFabricVersion(Version(strings.TrimSpace(tokens[1])))
	if lower == nil || upper == nil {
		return nil, fmt.Errorf("invalid Fabric range %q", raw)
	}
	return []SubExpr{
		{Value: lower, Operator: OpGte},
		{Value: upper, Operator: OpLte},
	}, nil
}

func parseFabricToken(raw string) ([]SubExpr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || isWildcardExpression(raw) {
		return nil, nil
	}

	// Comma-separated comparators are an AND group.
	if strings.Contains(raw, ",") {
		parts := strings.Split(raw, ",")
		terms := make([]SubExpr, 0, len(parts))
		for _, part := range parts {
			parsed, err := parseFabricToken(part)
			if err != nil {
				return nil, err
			}
			terms = append(terms, parsed...)
		}
		return terms, nil
	}

	operator := ""
	versionToken := raw
	for _, candidate := range []string{
		">=", "<=", "!=", "==", ">", "<", "=", "^", "~",
	} {
		if after, ok := strings.CutPrefix(raw, candidate); ok {
			operator = candidate
			versionToken = strings.TrimSpace(after)
			break
		}
	}
	if versionToken == "" {
		return nil, fmt.Errorf("invalid Fabric predicate %q", raw)
	}

	if hasWildcardComponent(versionToken) {
		switch operator {
		case "", "=", "==":
			return parseFabricWildcardRange(versionToken)
		default:
			return nil, fmt.Errorf(
				"Fabric wildcard predicate %q requires equality",
				raw,
			)
		}
	}

	lower := parseFabricVersion(Version(versionToken))
	if lower == nil {
		return nil, fmt.Errorf("invalid Fabric version %q", versionToken)
	}

	if _, semantic := lower.(*SemverVersion); !semantic {
		switch operator {
		case ">", "<", "!=":
			return nil, fmt.Errorf(
				"Fabric predicate %q needs a semantic version",
				raw,
			)
		default:
			// Fabric reduces inclusive predicates on plain strings to equality.
			return []SubExpr{
				{Value: lower, Operator: OpEq},
			}, nil
		}
	}

	switch operator {
	case "", "=", "==":
		return []SubExpr{{Value: lower, Operator: OpEq}}, nil
	case "!=":
		return []SubExpr{{Value: lower, Operator: OpNeq}}, nil
	case ">":
		return []SubExpr{{Value: lower, Operator: OpGt}}, nil
	case ">=":
		return []SubExpr{{Value: lower, Operator: OpGte}}, nil
	case "<":
		return []SubExpr{{Value: lower, Operator: OpLt}}, nil
	case "<=":
		return []SubExpr{{Value: lower, Operator: OpLte}}, nil
	case "^", "~":
		upper := semverUpperBound(lower.(*SemverVersion), operator)
		if upper == nil {
			return nil, fmt.Errorf("invalid Fabric predicate %q", raw)
		}
		return []SubExpr{
			{Value: lower, Operator: OpGte},
			{Value: upper, Operator: OpLt},
		}, nil
	default:
		return nil, fmt.Errorf("invalid Fabric predicate %q", raw)
	}
}

// parseFabricWildcardRange expands a Fabric X-range. Fabric includes
// prereleases at the lower boundary and excludes them at the next component.
func parseFabricWildcardRange(raw string) ([]SubExpr, error) {
	core := raw
	if index := strings.IndexByte(core, '+'); index >= 0 {
		core = core[:index]
	}
	if strings.Contains(core, "-") {
		return nil, fmt.Errorf(
			"Fabric wildcard %q cannot carry a prerelease",
			raw,
		)
	}

	parts := strings.Split(core, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("invalid Fabric wildcard %q", raw)
	}
	wildcardIndex := -1
	for index, part := range parts {
		if isWildcardComponent(part) {
			wildcardIndex = index
			break
		}
	}
	if wildcardIndex <= 0 {
		return nil, fmt.Errorf("invalid Fabric wildcard %q", raw)
	}
	for _, part := range parts[wildcardIndex:] {
		if !isWildcardComponent(part) {
			return nil, fmt.Errorf("invalid Fabric wildcard %q", raw)
		}
	}
	parts = parts[:wildcardIndex+1]

	numbers := make([]uint64, 0, wildcardIndex)
	for _, part := range parts[:wildcardIndex] {
		number, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid Fabric wildcard %q", raw)
		}
		numbers = append(numbers, number)
	}

	switch wildcardIndex {
	case 1: // a.x behaves as ^a-
		lower := newSemverBoundary(DialectFabric, numbers[0])
		upper := newSemverBoundary(DialectFabric, numbers[0]+1)
		return []SubExpr{
			{Value: lower, Operator: OpGte},
			{Value: upper, Operator: OpLt},
		}, nil
	case 2: // a.b.x behaves as ~a.b-
		lower := newSemverBoundary(
			DialectFabric,
			numbers[0],
			numbers[1],
		)
		upper := newSemverBoundary(
			DialectFabric,
			numbers[0],
			numbers[1]+1,
		)
		return []SubExpr{
			{Value: lower, Operator: OpGte},
			{Value: upper, Operator: OpLt},
		}, nil
	default: // a.b.c.x increments the last numeric component.
		lower := newSemverBoundary(DialectFabric, numbers...)
		incremented := append([]uint64(nil), numbers...)
		incremented[len(incremented)-1]++
		upper := newSemverBoundary(DialectFabric, incremented...)
		return []SubExpr{
			{Value: lower, Operator: OpGte},
			{Value: upper, Operator: OpLt},
		}, nil
	}
}

func semverUpperBound(
	lower *SemverVersion,
	operator string,
) ResolvableVersion {
	switch operator {
	case "^":
		return newSemverBoundary(
			lower.Dialect(),
			lower.Major()+1,
		)
	case "~":
		return newSemverBoundary(
			lower.Dialect(),
			lower.Major(),
			lower.Minor()+1,
		)
	default:
		return nil
	}
}

// parseMCDRConstraint parses metadata dependency requirements. Criterions are
// space-separated and combined with AND.
//
// References:
//   - https://docs.mcdreforged.com/en/latest/plugin_dev/metadata.html
//   - https://github.com/MCDReforged/MCDReforged/blob/master/mcdreforged/plugin/meta/version.py
func parseMCDRConstraint(raw string) (Expr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || isWildcardExpression(raw) {
		return nil, nil
	}

	tokens := strings.Fields(raw)
	terms := make([]SubExpr, 0, len(tokens))
	for _, token := range tokens {
		parsed, err := parseMCDRCriterion(token)
		if err != nil {
			return nil, err
		}
		terms = append(terms, parsed...)
	}
	if len(terms) == 0 {
		return nil, nil
	}
	return Expr{terms}, nil
}

func parseMCDRCriterion(raw string) ([]SubExpr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || isWildcardExpression(raw) {
		return nil, nil
	}

	operator := ""
	versionToken := raw
	for _, candidate := range []string{
		">=", "<=", "==", ">", "<", "=", "^", "~",
	} {
		if after, ok := strings.CutPrefix(raw, candidate); ok {
			operator = candidate
			versionToken = strings.TrimSpace(after)
			break
		}
	}
	if versionToken == "" {
		return nil, fmt.Errorf("invalid MCDR criterion %q", raw)
	}

	lower := parseMCDRVersion(versionToken, true)
	if lower == nil {
		return nil, fmt.Errorf("invalid MCDR version %q", versionToken)
	}
	semantic, ok := lower.(*SemverVersion)
	if !ok {
		return nil, fmt.Errorf("invalid MCDR version %q", versionToken)
	}

	switch operator {
	case "", "=", "==":
		return []SubExpr{{Value: lower, Operator: OpEq}}, nil
	case ">":
		return []SubExpr{{Value: lower, Operator: OpGt}}, nil
	case ">=":
		return []SubExpr{{Value: lower, Operator: OpGte}}, nil
	case "<":
		return []SubExpr{{Value: lower, Operator: OpLt}}, nil
	case "<=":
		return []SubExpr{{Value: lower, Operator: OpLte}}, nil
	case "^":
		if semantic.wildcardAt(0) {
			return []SubExpr{{Operator: OpNever}}, nil
		}
		upper := newSemverBoundary(
			DialectMCDR,
			semantic.Major()+1,
		)
		return []SubExpr{
			{Value: lower, Operator: OpGte},
			{Value: upper, Operator: OpLt},
		}, nil
	case "~":
		if semantic.wildcardAt(0) || semantic.wildcardAt(1) {
			return []SubExpr{{Operator: OpNever}}, nil
		}
		upper := newSemverBoundary(
			DialectMCDR,
			semantic.Major(),
			semantic.Minor()+1,
		)
		return []SubExpr{
			{Value: lower, Operator: OpGte},
			{Value: upper, Operator: OpLt},
		}, nil
	default:
		return nil, fmt.Errorf("invalid MCDR criterion %q", raw)
	}
}

func hasWildcardComponent(raw string) bool {
	core := raw
	if index := strings.IndexByte(core, '+'); index >= 0 {
		core = core[:index]
	}
	if index := strings.IndexByte(core, '-'); index >= 0 {
		core = core[:index]
	}
	for _, part := range strings.Split(core, ".") {
		if isWildcardComponent(part) {
			return true
		}
	}
	return false
}

func isWildcardExpression(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return true
	}
	return isWildcardComponent(token)
}

func isWildcardComponent(token string) bool {
	return token == "*" || token == "x" || token == "X"
}
