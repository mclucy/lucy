package version

import (
	"fmt"
	"strings"
)

// parseQuiltConstraint parses quilt.mod.json dependency specifiers. Space and
// comma separated predicates form one conjunction, and || separates
// alternatives.
func parseQuiltConstraint(raw string) (Expr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || isWildcardExpression(raw) {
		return nil, nil
	}

	alternatives := strings.Split(raw, "||")
	result := make(Expr, 0, len(alternatives))
	for _, alternative := range alternatives {
		alternative = strings.TrimSpace(alternative)
		if alternative == "" {
			return nil, fmt.Errorf("empty alternative in Quilt range %q", raw)
		}
		terms, err := parseQuiltClause(alternative)
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

func parseQuiltClause(raw string) ([]SubExpr, error) {
	if strings.Contains(raw, " - ") {
		return parseFabricHyphenRange(raw)
	}

	tokens := strings.FieldsFunc(
		raw,
		func(r rune) bool { return r == ' ' || r == '\t' || r == ',' },
	)
	terms := make([]SubExpr, 0, len(tokens))
	for _, token := range tokens {
		parsed, err := parseQuiltToken(token)
		if err != nil {
			return nil, err
		}
		terms = append(terms, parsed...)
	}
	return terms, nil
}

// parseQuiltToken parses one predicate. A bare version is the compatible range
// below the next major version; every other predicate follows Fabric.
func parseQuiltToken(raw string) ([]SubExpr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || isWildcardExpression(raw) {
		return nil, nil
	}
	if hasQuiltOperator(raw) || hasWildcardComponent(raw) {
		return parseFabricToken(raw)
	}

	lower := parseFabricVersion(Version(raw))
	semantic, ok := lower.(*SemverVersion)
	if !ok {
		return nil, fmt.Errorf("invalid Quilt version %q", raw)
	}
	upper := parseFabricVersion(
		Version(fmt.Sprintf("%d.0.0-", semantic.Major()+1)),
	)
	if upper == nil {
		return nil, fmt.Errorf("invalid Quilt version %q", raw)
	}
	return []SubExpr{
		{Value: lower, Operator: OpGte},
		{Value: upper, Operator: OpLt},
	}, nil
}

func hasQuiltOperator(raw string) bool {
	for _, prefix := range []string{
		">=", "<=", "!=", "==", ">", "<", "=", "^", "~",
	} {
		if strings.HasPrefix(raw, prefix) {
			return true
		}
	}
	return false
}
