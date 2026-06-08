package install

import "github.com/mclucy/lucy/types"

// ConstraintGraph is the merged requirement graph keyed by
// PackageId.StringPlatformName().
type ConstraintGraph map[string]ConstraintRequirement

// ConstraintRequirement is the merged requirement set for one package identity.
// Constraint contains the merged DNF expression, and Provenance preserves the
// requester that contributed each atomic clause.
type ConstraintRequirement struct {
	Id         types.PackageId
	Constraint types.VersionExpr
	Provenance []ConstraintProvenance

	variants []constraintVariant
}

// ConstraintProvenance records one atomic merged clause and the requester that
// introduced it.
type ConstraintProvenance struct {
	Requester  string
	Constraint types.VersionSubExpr
}

type constraintVariant struct {
	Clauses []ConstraintProvenance
}

type boundConstraint struct {
	Clause     ConstraintProvenance
	Inclusive  bool
	LowerBound bool
}

// MergeConstraintGraph is the pure constraint solver core for recursive
// installation. Callers must keep probing, routing, logging, and output outside
// this boundary and provide only in-memory constraint inputs.
//
// It merges all incoming constraint inputs by package identity, preserving
// requester provenance and returning a conflict error when the merged
// expression becomes unsatisfiable.
func MergeConstraintGraph(inputs []ConstraintInput) (ConstraintGraph, error) {
	graph := make(ConstraintGraph)

	for _, input := range inputs {
		id := input.Dependency.Id
		key := id.StringPlatformName()
		entry := graph[key]
		if entry.Id.Name == "" {
			entry.Id = types.PackageId{Platform: id.Platform, Name: id.Name}
			entry.variants = []constraintVariant{{}}
		}

		variants, err := constraintInputVariants(input)
		if err != nil {
			return nil, err
		}

		mergedVariants, mergeErr := mergeRequirementVariants(
			entry.Id,
			entry.variants,
			variants,
		)
		if mergeErr != nil {
			return nil, mergeErr
		}

		entry.variants = mergedVariants
		entry.Constraint = variantsToExpression(mergedVariants)
		entry.Provenance = flattenProvenance(mergedVariants)
		graph[key] = entry
	}

	return graph, nil
}

// IsSatisfied reports whether the merged requirement for id accepts version.
func (g ConstraintGraph) IsSatisfied(
id types.PackageId,
version types.ResolvableVersion,
) bool {
	entry, ok := g[id.StringPlatformName()]
	if !ok {
		return false
	}
	dep := types.Dependency{
		Id: entry.Id, Constraint: entry.Constraint, Mandatory: true,
	}
	return dep.Satisfy(id, version)
}

func mergeRequirementVariants(
id types.PackageId,
left []constraintVariant,
right []constraintVariant,
) ([]constraintVariant, error) {
	if len(left) == 0 {
		left = []constraintVariant{{}}
	}
	if len(right) == 0 {
		right = []constraintVariant{{}}
	}

	merged := make([]constraintVariant, 0, len(left)*len(right))
	var firstConflict *ConstraintConflictError
	for _, leftVariant := range left {
		for _, rightVariant := range right {
			combined := constraintVariant{
				Clauses: make(
					[]ConstraintProvenance,
					0,
					len(leftVariant.Clauses)+len(rightVariant.Clauses),
				),
			}
			combined.Clauses = append(combined.Clauses, leftVariant.Clauses...)
			combined.Clauses = append(combined.Clauses, rightVariant.Clauses...)
			ok, conflict := conjunctionSatisfiable(id, combined.Clauses)
			if ok {
				merged = append(merged, combined)
				continue
			}
			if firstConflict == nil {
				firstConflict = conflict
			}
		}
	}

	if len(merged) == 0 {
		if firstConflict != nil {
			return nil, firstConflict
		}
		return nil, &ConstraintConflictError{PackageId: id}
	}

	return merged, nil
}

func conjunctionSatisfiable(
id types.PackageId,
clauses []ConstraintProvenance,
) (bool, *ConstraintConflictError) {
	var eq *ConstraintProvenance
	var lower *boundConstraint
	var upper *boundConstraint
	neqs := make([]ConstraintProvenance, 0)

	for i := range clauses {
		clause := clauses[i]
		switch clause.Constraint.Operator {
		case types.OpEq:
			if eq == nil {
				eq = &clause
				continue
			}
			cmp, ok := eq.Constraint.Value.Compare(clause.Constraint.Value)
			if ok && cmp != 0 {
				return false, conflictFor(id, *eq, clause)
			}
		case types.OpNeq:
			neqs = append(neqs, clause)
		case types.OpGt:
			lower = strongerLower(
				lower,
				boundConstraint{Clause: clause, LowerBound: true},
			)
		case types.OpGte:
			lower = strongerLower(
				lower,
				boundConstraint{
					Clause: clause, Inclusive: true, LowerBound: true,
				},
			)
		case types.OpLt:
			upper = strongerUpper(upper, boundConstraint{Clause: clause})
		case types.OpLte:
			upper = strongerUpper(
				upper,
				boundConstraint{Clause: clause, Inclusive: true},
			)
		}
	}

	if eq != nil {
		for _, neq := range neqs {
			cmp, ok := eq.Constraint.Value.Compare(neq.Constraint.Value)
			if ok && cmp == 0 {
				return false, conflictFor(id, *eq, neq)
			}
		}
		for _, clause := range clauses {
			if clause.Constraint.Operator == types.OpEq {
				continue
			}
			cmp := clause.Constraint.Operator.Comparator()
			if !cmp(eq.Constraint.Value, clause.Constraint.Value) {
				return false, conflictFor(id, *eq, clause)
			}
		}
		return true, nil
	}

	if lower != nil && upper != nil {
		cmp, ok := lower.Clause.Constraint.Value.Compare(upper.Clause.Constraint.Value)
		if ok {
			if cmp > 0 {
				return false, conflictFor(id, lower.Clause, upper.Clause)
			}
			if cmp == 0 && (!lower.Inclusive || !upper.Inclusive) {
				return false, conflictFor(id, lower.Clause, upper.Clause)
			}
		}
	}

	return true, nil
}

func strongerLower(
current *boundConstraint,
candidate boundConstraint,
) *boundConstraint {
	if current == nil {
		return &candidate
	}
	cmp, ok := current.Clause.Constraint.Value.Compare(candidate.Clause.Constraint.Value)
	if !ok {
		return current
	}
	if cmp < 0 {
		return &candidate
	}
	if cmp > 0 {
		return current
	}
	if current.Inclusive && !candidate.Inclusive {
		return &candidate
	}
	return current
}

func strongerUpper(
current *boundConstraint,
candidate boundConstraint,
) *boundConstraint {
	if current == nil {
		return &candidate
	}
	cmp, ok := current.Clause.Constraint.Value.Compare(candidate.Clause.Constraint.Value)
	if !ok {
		return current
	}
	if cmp > 0 {
		return &candidate
	}
	if cmp < 0 {
		return current
	}
	if current.Inclusive && !candidate.Inclusive {
		return &candidate
	}
	return current
}

func conflictFor(
id types.PackageId,
left, right ConstraintProvenance,
) *ConstraintConflictError {
	return &ConstraintConflictError{
		PackageId: id,
		Left: ConstraintConflictSource{
			Requester: left.Requester, Constraint: left.Constraint,
		},
		Right: ConstraintConflictSource{
			Requester: right.Requester, Constraint: right.Constraint,
		},
	}
}
