package version

// Expr is a boolean expression in disjunctive normal form: the outer slice is
// OR, the inner slice is AND. A nil or empty outer slice means the expression
// imposes no constraint.
type Expr [][]SubExpr

type SubExpr struct {
	Value    ResolvableVersion
	Operator Operator
	// Inverted records that Operator is the logical complement of a parsed
	// predicate. Dialects with partial ordering use it to keep the complement
	// consistent with their own predicate rules.
	Inverted bool
}

// Negate returns the logical negation of the expression in disjunctive normal
// form. Negating a disjunction produces a conjunction of negated clauses, so
// the result is distributed back into OR-of-AND form.
func (exps Expr) Negate() Expr {
	if len(exps) == 0 {
		return Expr{{SubExpr{Operator: OpNever}}}
	}

	result := Expr{{}}
	for _, conjunction := range exps {
		if len(conjunction) == 0 {
			return Expr{{SubExpr{Operator: OpNever}}}
		}

		clause := make([]SubExpr, 0, len(conjunction))
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

		next := make(Expr, 0, len(result)*len(clause))
		for _, product := range result {
			for _, term := range clause {
				distributed := make([]SubExpr, 0, len(product)+1)
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
		return Expr{{SubExpr{Operator: OpNever}}}
	}
	return result
}

// negated returns the logical complement of one term. always reports a term
// that is already true, whose negation contributes nothing to a conjunction.
func (term SubExpr) negated() (inverted SubExpr, always bool) {
	if term.Operator == OpNever || term.Value == nil {
		return SubExpr{}, true
	}
	return SubExpr{
		Value:    term.Value,
		Operator: term.Operator.Inverse(),
		Inverted: !term.Inverted,
	}, false
}

type Operator uint8

const (
	OpEq Operator = iota
	OpNeq
	OpGt
	OpGte
	OpLt
	OpLte
	// OpNever is the canonical unsatisfiable term produced by negating an
	// unconstrained expression.
	OpNever
)

func (op Operator) String() string {
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

func (op Operator) ToSign() string {
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

func (op Operator) Inverse() Operator {
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
