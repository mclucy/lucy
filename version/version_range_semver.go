package version

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/types"
)

// parseFabricConstraint parses fabric.mod.json dependency predicates. Each
// entry of a VersionRange array is an alternative, which ParseConstraints
// merges into OR clauses.
//
// Reference: https://wiki.fabricmc.net/documentation:fabric_mod_json_spec
func parseFabricConstraint(raw string) (types.VersionExpr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || isWildcardExpression(raw) {
		return nil, nil
	}

	parts := strings.Split(raw, "||")
	result := make(types.VersionExpr, 0, len(parts))
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

func parseFabricClause(raw string) ([]types.VersionSubExpr, error) {
	if strings.Contains(raw, " - ") {
		return parseFabricHyphenRange(raw)
	}

	tokens := strings.Fields(raw)
	terms := make([]types.VersionSubExpr, 0, len(tokens))
	for _, token := range tokens {
		parsed, err := parseFabricToken(token)
		if err != nil {
			return nil, err
		}
		terms = append(terms, parsed...)
	}
	return terms, nil
}

func parseFabricHyphenRange(raw string) ([]types.VersionSubExpr, error) {
	tokens := strings.SplitN(raw, " - ", 2)
	if len(tokens) != 2 {
		return nil, fmt.Errorf("invalid Fabric range %q", raw)
	}
	lower := parseFabricVersion(types.Version(strings.TrimSpace(tokens[0])))
	upper := parseFabricVersion(types.Version(strings.TrimSpace(tokens[1])))
	if lower == nil || upper == nil {
		return nil, fmt.Errorf("invalid Fabric range %q", raw)
	}
	return []types.VersionSubExpr{
		{Value: lower, Operator: types.OpGte},
		{Value: upper, Operator: types.OpLte},
	}, nil
}

func parseFabricToken(raw string) ([]types.VersionSubExpr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || isWildcardExpression(raw) {
		return nil, nil
	}

	// Comma-separated comparators are an AND group.
	if strings.Contains(raw, ",") {
		parts := strings.Split(raw, ",")
		terms := make([]types.VersionSubExpr, 0, len(parts))
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

	lower := parseFabricVersion(types.Version(versionToken))
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
			return []types.VersionSubExpr{
				{Value: lower, Operator: types.OpEq},
			}, nil
		}
	}

	switch operator {
	case "", "=", "==":
		return []types.VersionSubExpr{{Value: lower, Operator: types.OpEq}}, nil
	case "!=":
		return []types.VersionSubExpr{{Value: lower, Operator: types.OpNeq}}, nil
	case ">":
		return []types.VersionSubExpr{{Value: lower, Operator: types.OpGt}}, nil
	case ">=":
		return []types.VersionSubExpr{{Value: lower, Operator: types.OpGte}}, nil
	case "<":
		return []types.VersionSubExpr{{Value: lower, Operator: types.OpLt}}, nil
	case "<=":
		return []types.VersionSubExpr{{Value: lower, Operator: types.OpLte}}, nil
	case "^", "~":
		upper := semverUpperBound(lower.(*SemverVersion), operator)
		if upper == nil {
			return nil, fmt.Errorf("invalid Fabric predicate %q", raw)
		}
		return []types.VersionSubExpr{
			{Value: lower, Operator: types.OpGte},
			{Value: upper, Operator: types.OpLt},
		}, nil
	default:
		return nil, fmt.Errorf("invalid Fabric predicate %q", raw)
	}
}

// parseFabricWildcardRange expands a Fabric X-range. Fabric includes
// prereleases at the lower boundary and excludes them at the next component.
func parseFabricWildcardRange(raw string) ([]types.VersionSubExpr, error) {
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
		lower := newSemverBoundary(types.VersionDialectFabric, numbers[0])
		upper := newSemverBoundary(types.VersionDialectFabric, numbers[0]+1)
		return []types.VersionSubExpr{
			{Value: lower, Operator: types.OpGte},
			{Value: upper, Operator: types.OpLt},
		}, nil
	case 2: // a.b.x behaves as ~a.b-
		lower := newSemverBoundary(
			types.VersionDialectFabric,
			numbers[0],
			numbers[1],
		)
		upper := newSemverBoundary(
			types.VersionDialectFabric,
			numbers[0],
			numbers[1]+1,
		)
		return []types.VersionSubExpr{
			{Value: lower, Operator: types.OpGte},
			{Value: upper, Operator: types.OpLt},
		}, nil
	default: // a.b.c.x increments the last numeric component.
		lower := newSemverBoundary(types.VersionDialectFabric, numbers...)
		incremented := append([]uint64(nil), numbers...)
		incremented[len(incremented)-1]++
		upper := newSemverBoundary(types.VersionDialectFabric, incremented...)
		return []types.VersionSubExpr{
			{Value: lower, Operator: types.OpGte},
			{Value: upper, Operator: types.OpLt},
		}, nil
	}
}

func semverUpperBound(
	lower *SemverVersion,
	operator string,
) types.ResolvableVersion {
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
func parseMCDRConstraint(raw string) (types.VersionExpr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || isWildcardExpression(raw) {
		return nil, nil
	}

	tokens := strings.Fields(raw)
	terms := make([]types.VersionSubExpr, 0, len(tokens))
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
	return types.VersionExpr{terms}, nil
}

func parseMCDRCriterion(raw string) ([]types.VersionSubExpr, error) {
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
		return []types.VersionSubExpr{{Value: lower, Operator: types.OpEq}}, nil
	case ">":
		return []types.VersionSubExpr{{Value: lower, Operator: types.OpGt}}, nil
	case ">=":
		return []types.VersionSubExpr{{Value: lower, Operator: types.OpGte}}, nil
	case "<":
		return []types.VersionSubExpr{{Value: lower, Operator: types.OpLt}}, nil
	case "<=":
		return []types.VersionSubExpr{{Value: lower, Operator: types.OpLte}}, nil
	case "^":
		if semantic.wildcardAt(0) {
			return []types.VersionSubExpr{{Operator: types.OpNever}}, nil
		}
		upper := newSemverBoundary(
			types.VersionDialectMCDR,
			semantic.Major()+1,
		)
		return []types.VersionSubExpr{
			{Value: lower, Operator: types.OpGte},
			{Value: upper, Operator: types.OpLt},
		}, nil
	case "~":
		if semantic.wildcardAt(0) || semantic.wildcardAt(1) {
			return []types.VersionSubExpr{{Operator: types.OpNever}}, nil
		}
		upper := newSemverBoundary(
			types.VersionDialectMCDR,
			semantic.Major(),
			semantic.Minor()+1,
		)
		return []types.VersionSubExpr{
			{Value: lower, Operator: types.OpGte},
			{Value: upper, Operator: types.OpLt},
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
