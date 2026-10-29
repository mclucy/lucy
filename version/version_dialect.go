package version

import (
	"fmt"
	"strings"
)

// ErrAmbiguousVersion is returned when attempting to parse a non-exact
// version constant.
var ErrAmbiguousVersion = fmt.Errorf("attempting to parse an ambiguous version")

// Engine parses, orders, and matches versions under one grammar.
type Engine struct {
	kind Dialect
}

// NewEngine binds operations to a dialect identifier.
func NewEngine(kind Dialect) (Engine, error) {
	if !kind.Valid() {
		return Engine{}, fmt.Errorf("unsupported version dialect %q", kind)
	}
	return Engine{kind: kind}, nil
}

// ParseVersion parses a concrete version. It rejects selectors, versions
// containing wildcards, and input outside the dialect grammar.
func (d Engine) ParseVersion(raw Version) (ResolvableVersion, error) {
	switch raw {
	case Beta, Stable, None, Any, Unknown:
		return nil, fmt.Errorf("%w: %s", ErrAmbiguousVersion, raw)
	}

	var parsed ResolvableVersion
	switch d.kind {
	case DialectFabric:
		parsed = parseFabricVersion(raw)
	case DialectMCDR:
		parsed = parseMCDRVersion(string(raw), false)
	case DialectMaven:
		parsed = parseMavenVersion(raw)
	case DialectMinecraft:
		parsed = parseMinecraftRelease(raw)
		if parsed == nil {
			parsed = parseMinecraftSnapshot(raw)
		}
	case DialectOpaque:
		parsed = parseOpaqueVersion(raw)
	default:
		return nil, fmt.Errorf("unsupported version dialect %q", d.kind)
	}
	if parsed == nil || !parsed.Validate() {
		return nil, fmt.Errorf(
			"invalid %s version %q",
			d.kind,
			raw,
		)
	}
	return parsed, nil
}

// ParseConstraint parses a version constraint. A nil expression with a nil
// error means the constraint accepts every version. Invalid input returns an
// error, never an unconstrained expression.
func (d Engine) ParseConstraint(raw string) (Expr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" {
		return nil, nil
	}
	switch d.kind {
	case DialectFabric:
		return parseFabricConstraint(raw)
	case DialectMCDR:
		return parseMCDRConstraint(raw)
	case DialectMaven:
		return parseMavenRange(raw)
	case DialectMinecraft:
		return parseMinecraftConstraint(raw)
	case DialectOpaque:
		return parseOpaqueConstraint(raw)
	default:
		return nil, fmt.Errorf("unsupported version dialect %q", d.kind)
	}
}

// ParseConstraints parses each string as an alternative. One unconstrained
// alternative makes the whole expression unconstrained.
func (d Engine) ParseConstraints(raws []string) (Expr, error) {
	if len(raws) == 0 {
		return nil, nil
	}

	merged := make(Expr, 0, len(raws))
	for _, raw := range raws {
		expr, err := d.ParseConstraint(raw)
		if err != nil {
			return nil, err
		}
		if len(expr) == 0 {
			return nil, nil
		}
		merged = append(merged, expr...)
	}
	if len(merged) == 0 {
		return nil, nil
	}
	return merged, nil
}

// Compare orders two versions of this dialect. Different dialects are never
// comparable.
func (d Engine) Compare(
	left, right ResolvableVersion,
) (int, bool) {
	if left == nil || right == nil {
		return 0, false
	}
	if left.Dialect() != d.kind || right.Dialect() != d.kind {
		return 0, false
	}
	if comparison, ok := left.Compare(right); ok {
		return comparison, true
	}
	if comparison, ok := right.Compare(left); ok {
		return -comparison, true
	}
	return 0, false
}

// Match reports whether a parsed version satisfies an expression of this
// dialect. A nil or empty expression accepts every version.
func (d Engine) Match(
	expr Expr,
	actual ResolvableVersion,
) bool {
	if len(expr) == 0 {
		return true
	}
	if actual == nil || actual.Dialect() != d.kind {
		return false
	}

	for _, conjunction := range expr {
		matched := true
		for _, term := range conjunction {
			if !d.matchTerm(term, actual) {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// MatchText parses both sides and reports whether the version satisfies the
// constraint.
func (d Engine) MatchText(
	raw string,
	actual Version,
) (bool, error) {
	expr, err := d.ParseConstraint(raw)
	if err != nil {
		return false, err
	}
	if len(expr) == 0 {
		return true, nil
	}
	parsed, err := d.ParseVersion(actual)
	if err != nil {
		return false, err
	}
	return d.Match(expr, parsed), nil
}

func (d Engine) matchTerm(
	term SubExpr,
	actual ResolvableVersion,
) bool {
	if term.Operator == OpNever || term.Value == nil {
		return false
	}
	if term.Value.Dialect() != d.kind {
		return false
	}

	if d.kind == DialectFabric {
		_, actualSemantic := actual.(*SemverVersion)
		_, boundSemantic := term.Value.(*SemverVersion)
		if !actualSemantic || !boundSemantic {
			return matchOpaqueOperator(
				term.Operator,
				term.Inverted,
				actual,
				term.Value,
			)
		}
	}

	comparison, ok := d.Compare(actual, term.Value)
	if !ok {
		// An incomparable operand makes the parsed predicate false, so its
		// complement is true.
		return term.Inverted
	}
	switch term.Operator {
	case OpEq:
		return comparison == 0
	case OpNeq:
		return comparison != 0
	case OpGt:
		return comparison > 0
	case OpGte:
		return comparison >= 0
	case OpLt:
		return comparison < 0
	case OpLte:
		return comparison <= 0
	default:
		return false
	}
}

// matchOpaqueOperator applies Fabric Loader's rule for versions outside its
// semantic grammar: an inclusive predicate requires exact string equality,
// while an exclusive predicate never matches. For a term produced by
// negation, the result is the complement of the original predicate.
func matchOpaqueOperator(
	operator Operator,
	inverted bool,
	left, right ResolvableVersion,
) bool {
	leftString, leftOK := left.(fmt.Stringer)
	rightString, rightOK := right.(fmt.Stringer)
	if !leftOK || !rightOK {
		return false
	}
	equal := leftString.String() == rightString.String()
	if inverted {
		return !matchOpaqueRaw(operator.Inverse(), equal)
	}
	return matchOpaqueRaw(operator, equal)
}

func matchOpaqueRaw(operator Operator, equal bool) bool {
	switch operator {
	case OpEq, OpGte, OpLte:
		return equal
	default:
		return false
	}
}

// ParseVersion parses a concrete version under dialect.
func ParseVersion(
	raw Version,
	dialect Dialect,
) (ResolvableVersion, error) {
	bound, err := NewEngine(dialect)
	if err != nil {
		return nil, err
	}
	return bound.ParseVersion(raw)
}

// ParseConstraint parses a constraint under dialect.
func ParseConstraint(
	raw string,
	dialect Dialect,
) (Expr, error) {
	bound, err := NewEngine(dialect)
	if err != nil {
		return nil, err
	}
	return bound.ParseConstraint(raw)
}

// ParseConstraints parses alternative constraints under dialect.
func ParseConstraints(
	raws []string,
	dialect Dialect,
) (Expr, error) {
	bound, err := NewEngine(dialect)
	if err != nil {
		return nil, err
	}
	return bound.ParseConstraints(raws)
}

// Compare orders two versions. Versions from different dialects are
// incomparable.
func Compare(left, right ResolvableVersion) (int, bool) {
	if left == nil || right == nil {
		return 0, false
	}
	bound, err := NewEngine(left.Dialect())
	if err != nil || left.Dialect() != right.Dialect() {
		return 0, false
	}
	return bound.Compare(left, right)
}

// Match reports whether actual satisfies raw under dialect.
func Match(
	raw string,
	actual Version,
	dialect Dialect,
) (bool, error) {
	bound, err := NewEngine(dialect)
	if err != nil {
		return false, err
	}
	return bound.MatchText(raw, actual)
}
