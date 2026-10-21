package version

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/types"
)

// ErrAmbiguousVersion is returned when attempting to parse a non-exact
// version constant.
var ErrAmbiguousVersion = fmt.Errorf("attempting to parse an ambiguous version")

// VersionDialect parses, orders, and matches versions under one grammar.
type VersionDialect struct {
	kind types.VersionDialect
}

// NewVersionDialect binds operations to a dialect identifier.
func NewVersionDialect(kind types.VersionDialect) (VersionDialect, error) {
	if !kind.Valid() {
		return VersionDialect{}, fmt.Errorf("unsupported version dialect %q", kind)
	}
	return VersionDialect{kind: kind}, nil
}

// ParseVersion parses a concrete version. It rejects selectors, versions
// containing wildcards, and input outside the dialect grammar.
func (d VersionDialect) ParseVersion(raw types.Version) (types.ResolvableVersion, error) {
	switch raw {
	case types.VersionBeta, types.VersionStable, types.VersionNone, types.VersionAny, types.VersionUnknown:
		return nil, fmt.Errorf("%w: %s", ErrAmbiguousVersion, raw)
	}

	var parsed types.ResolvableVersion
	switch d.kind {
	case types.VersionDialectFabric:
		parsed = parseFabricVersion(raw)
	case types.VersionDialectMCDR:
		parsed = parseMCDRVersion(string(raw), false)
	case types.VersionDialectMaven:
		parsed = parseMavenVersion(raw)
	case types.VersionDialectMinecraft:
		parsed = parseMinecraftRelease(raw)
		if parsed == nil {
			parsed = parseMinecraftSnapshot(raw)
		}
	case types.VersionDialectOpaque:
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
func (d VersionDialect) ParseConstraint(raw string) (types.VersionExpr, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "*" {
		return nil, nil
	}
	switch d.kind {
	case types.VersionDialectFabric:
		return parseFabricConstraint(raw)
	case types.VersionDialectMCDR:
		return parseMCDRConstraint(raw)
	case types.VersionDialectMaven:
		return parseMavenRange(raw)
	case types.VersionDialectMinecraft:
		return parseMinecraftConstraint(raw)
	case types.VersionDialectOpaque:
		return parseOpaqueConstraint(raw)
	default:
		return nil, fmt.Errorf("unsupported version dialect %q", d.kind)
	}
}

// ParseConstraints parses each string as an alternative. One unconstrained
// alternative makes the whole expression unconstrained.
func (d VersionDialect) ParseConstraints(raws []string) (types.VersionExpr, error) {
	if len(raws) == 0 {
		return nil, nil
	}

	merged := make(types.VersionExpr, 0, len(raws))
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
func (d VersionDialect) Compare(
	left, right types.ResolvableVersion,
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
func (d VersionDialect) Match(
	expr types.VersionExpr,
	actual types.ResolvableVersion,
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
func (d VersionDialect) MatchText(
	raw string,
	actual types.Version,
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

func (d VersionDialect) matchTerm(
	term types.VersionSubExpr,
	actual types.ResolvableVersion,
) bool {
	if term.Operator == types.OpNever || term.Value == nil {
		return false
	}
	if term.Value.Dialect() != d.kind {
		return false
	}

	if d.kind == types.VersionDialectFabric {
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
	case types.OpEq:
		return comparison == 0
	case types.OpNeq:
		return comparison != 0
	case types.OpGt:
		return comparison > 0
	case types.OpGte:
		return comparison >= 0
	case types.OpLt:
		return comparison < 0
	case types.OpLte:
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
	operator types.VersionOperator,
	inverted bool,
	left, right types.ResolvableVersion,
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

func matchOpaqueRaw(operator types.VersionOperator, equal bool) bool {
	switch operator {
	case types.OpEq, types.OpGte, types.OpLte:
		return equal
	default:
		return false
	}
}

// ParseVersion parses a concrete version under dialect.
func ParseVersion(
	raw types.Version,
	dialect types.VersionDialect,
) (types.ResolvableVersion, error) {
	bound, err := NewVersionDialect(dialect)
	if err != nil {
		return nil, err
	}
	return bound.ParseVersion(raw)
}

// ParseConstraint parses a constraint under dialect.
func ParseConstraint(
	raw string,
	dialect types.VersionDialect,
) (types.VersionExpr, error) {
	bound, err := NewVersionDialect(dialect)
	if err != nil {
		return nil, err
	}
	return bound.ParseConstraint(raw)
}

// ParseConstraints parses alternative constraints under dialect.
func ParseConstraints(
	raws []string,
	dialect types.VersionDialect,
) (types.VersionExpr, error) {
	bound, err := NewVersionDialect(dialect)
	if err != nil {
		return nil, err
	}
	return bound.ParseConstraints(raws)
}

// Compare orders two versions. Versions from different dialects are
// incomparable.
func Compare(left, right types.ResolvableVersion) (int, bool) {
	if left == nil || right == nil {
		return 0, false
	}
	bound, err := NewVersionDialect(left.Dialect())
	if err != nil || left.Dialect() != right.Dialect() {
		return 0, false
	}
	return bound.Compare(left, right)
}

// Match reports whether actual satisfies raw under dialect.
func Match(
	raw string,
	actual types.Version,
	dialect types.VersionDialect,
) (bool, error) {
	bound, err := NewVersionDialect(dialect)
	if err != nil {
		return false, err
	}
	return bound.MatchText(raw, actual)
}
