package types

// DependencyOrigin identifies how a dependency is satisfied.
type DependencyOrigin string

const (
	DependencyRegular        DependencyOrigin = "regular"
	DependencyServerProvided DependencyOrigin = "server-provided"
	DependencyEmbedded       DependencyOrigin = "embedded"
)

func NormalizeDependencyOrigin(origin DependencyOrigin) DependencyOrigin {
	if origin == "" {
		return DependencyRegular
	}
	return origin
}

func (d Dependency) IsServerProvided() bool {
	return NormalizeDependencyOrigin(d.Origin) == DependencyServerProvided
}

func (d Dependency) IsEmbedded() bool {
	return NormalizeDependencyOrigin(d.Origin) == DependencyEmbedded
}

// Dependency is a package dependency request. Constraint is a 2D array whose
// outer array is OR and inner array is AND; nil or empty means no constraint.
// ID.Version is always empty because Constraint carries the version selector.
//
// DependencyOrigin records how the dependency is satisfied. Regular
// dependencies resolve through upstream providers, server-provided
// dependencies come from the server environment, and embedded dependencies
// are physically bundled inside the parent artifact.
type Dependency struct {
	ID         VersionedPackageRef
	Constraint VersionExpr
	Mandatory  bool
	Origin     DependencyOrigin
}

// VersionExpr is a boolean expression in disjunctive normal form: the outer
// slice is OR, the inner slice is AND. A nil or empty outer slice means the
// expression imposes no constraint.
type VersionExpr [][]VersionSubExpr

type VersionSubExpr struct {
	Value    ResolvableVersion
	Operator VersionOperator
	// Inverted records that Operator is the logical complement of a parsed
	// predicate. Dialects with partial ordering use it to keep the complement
	// consistent with their own predicate rules.
	Inverted bool
}

// Negate returns the logical negation of the expression in disjunctive normal
// form. Negating a disjunction produces a conjunction of negated clauses, so
// the result is distributed back into OR-of-AND form.
func (exps VersionExpr) Negate() VersionExpr {
	if len(exps) == 0 {
		return VersionExpr{{VersionSubExpr{Operator: OpNever}}}
	}

	result := VersionExpr{{}}
	for _, conjunction := range exps {
		if len(conjunction) == 0 {
			return VersionExpr{{VersionSubExpr{Operator: OpNever}}}
		}

		clause := make([]VersionSubExpr, 0, len(conjunction))
		trueClause := false
		for _, term := range conjunction {
			inverted, always := term.negated()
			if always {
				trueClause = true
				break
			}
			clause = append(clause, inverted)
		}
		if trueClause {
			continue
		}

		next := make(VersionExpr, 0, len(result)*len(clause))
		for _, product := range result {
			for _, term := range clause {
				distributed := make([]VersionSubExpr, 0, len(product)+1)
				distributed = append(distributed, product...)
				distributed = append(distributed, term)
				next = append(next, distributed)
			}
		}
		result = next
	}

	if len(result) == 1 && len(result[0]) == 0 {
		return nil
	}
	if len(result) == 0 {
		return VersionExpr{{VersionSubExpr{Operator: OpNever}}}
	}
	return result
}

// negated returns the logical complement of one term. always reports a term
// that is already true, whose negation contributes nothing to a conjunction.
func (term VersionSubExpr) negated() (inverted VersionSubExpr, always bool) {
	if term.Operator == OpNever || term.Value == nil {
		return VersionSubExpr{}, true
	}
	return VersionSubExpr{
		Value:    term.Value,
		Operator: term.Operator.Inverse(),
		Inverted: !term.Inverted,
	}, false
}

type VersionOperator uint8

const (
	OpEq VersionOperator = iota
	OpNeq
	OpGt
	OpGte
	OpLt
	OpLte
	// OpNever is the canonical unsatisfiable term produced by negating an
	// unconstrained expression.
	OpNever
)

func (op VersionOperator) String() string {
	switch op {
	case OpEq:
		return "equal"
	case OpNeq:
		return "not equal"
	case OpGt:
		return "greater than"
	case OpGte:
		return "greater than or equal"
	case OpLt:
		return "less than"
	case OpLte:
		return "less than or equal"
	case OpNever:
		return "never"
	default:
		return "unknown"
	}
}

func (op VersionOperator) ToSign() string {
	switch op {
	case OpEq:
		return "="
	case OpNeq:
		return "!="
	case OpGt:
		return ">"
	case OpGte:
		return ">="
	case OpLt:
		return "<"
	case OpLte:
		return "<="
	case OpNever:
		return "!"
	default:
		return "unknown"
	}
}

func (op VersionOperator) Inverse() VersionOperator {
	switch op {
	case OpEq:
		return OpNeq
	case OpNeq:
		return OpEq
	case OpGt:
		return OpLte
	case OpGte:
		return OpLt
	case OpLt:
		return OpGte
	case OpLte:
		return OpGt
	default:
		return op
	}
}
