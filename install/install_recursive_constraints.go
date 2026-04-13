package install

import (
	"fmt"

	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/types"
)

// ConstraintGraph is the merged requirement graph keyed by
// PackageId.StringPlatformName().
type ConstraintGraph map[string]ConstraintRequirement

// ConstraintRequirement is the merged requirement set for one package identity.
// Constraint contains the merged DNF expression, and Provenance preserves the
// requester that contributed each atomic clause.
type ConstraintRequirement struct {
	Id         types.PackageId
	Constraint types.VersionConstraintExpression
	Provenance []ConstraintProvenance

	variants []constraintVariant
}

// ConstraintProvenance records one atomic merged clause and the requester that
// introduced it.
type ConstraintProvenance struct {
	Requester  string
	Constraint types.VersionConstraint
}

type constraintVariant struct {
	Clauses []ConstraintProvenance
}

type boundConstraint struct {
	Clause     ConstraintProvenance
	Inclusive  bool
	LowerBound bool
}

// MergeConstraintGraph merges all incoming constraint inputs by package
// identity, preserving requester provenance and returning a conflict error when
// the merged expression becomes unsatisfiable.
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

		mergedVariants, mergeErr := mergeRequirementVariants(entry.Id, entry.variants, variants)
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
func (g ConstraintGraph) IsSatisfied(id types.PackageId, version types.ComparableVersion) bool {
	entry, ok := g[id.StringPlatformName()]
	if !ok {
		return false
	}
	dep := types.Dependency{Id: entry.Id, Constraint: entry.Constraint, Mandatory: true}
	return dep.Satisfy(id, version)
}

func constraintInputVariants(input ConstraintInput) ([]constraintVariant, error) {
	expr, err := normalizeConstraintExpression(input.Dependency)
	if err != nil {
		return nil, err
	}
	variants := make([]constraintVariant, 0, len(expr))
	for _, group := range expr {
		variant := constraintVariant{Clauses: make([]ConstraintProvenance, 0, len(group))}
		for _, clause := range group {
			expanded, expandErr := expandConstraint(clause)
			if expandErr != nil {
				return nil, expandErr
			}
			for _, expandedClause := range expanded {
				variant.Clauses = append(variant.Clauses, ConstraintProvenance{
					Requester:  input.Requester,
					Constraint: expandedClause,
				})
			}
		}
		variants = append(variants, variant)
	}
	if len(variants) == 0 {
		return []constraintVariant{{}}, nil
	}
	return variants, nil
}

func normalizeConstraintExpression(dep types.Dependency) (types.VersionConstraintExpression, error) {
	if len(dep.Constraint) > 0 {
		return cloneConstraintExpression(dep.Constraint), nil
	}
	if dep.Id.Version == "" || dep.Id.Version == types.VersionAny || dep.Id.Version.IsInvalid() || dep.Id.Version.CanInfer() {
		return types.VersionConstraintExpression{{}}, nil
	}
	value := dependency.Parse(dep.Id.Version, defaultVersionScheme(dep.Id))
	if value == nil {
		return nil, fmt.Errorf("install: failed to parse fixed constraint version %q for %s", dep.Id.Version, dep.Id.StringPlatformName())
	}
	return types.VersionConstraintExpression{{{
		Operator: types.OpEq,
		Value:    value,
	}}}, nil
}

func defaultVersionScheme(id types.PackageId) types.VersionScheme {
	if id.Platform == types.PlatformMinecraft {
		return types.MinecraftRelease
	}
	return types.Semver
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
			combined := constraintVariant{Clauses: make([]ConstraintProvenance, 0, len(leftVariant.Clauses)+len(rightVariant.Clauses))}
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

func conjunctionSatisfiable(id types.PackageId, clauses []ConstraintProvenance) (bool, *ConstraintConflictError) {
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
			lower = strongerLower(lower, boundConstraint{Clause: clause, LowerBound: true})
		case types.OpGte:
			lower = strongerLower(lower, boundConstraint{Clause: clause, Inclusive: true, LowerBound: true})
		case types.OpLt:
			upper = strongerUpper(upper, boundConstraint{Clause: clause})
		case types.OpLte:
			upper = strongerUpper(upper, boundConstraint{Clause: clause, Inclusive: true})
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

func strongerLower(current *boundConstraint, candidate boundConstraint) *boundConstraint {
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

func strongerUpper(current *boundConstraint, candidate boundConstraint) *boundConstraint {
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

func expandConstraint(clause types.VersionConstraint) ([]types.VersionConstraint, error) {
	switch clause.Operator {
	case types.OpWeakEq:
		lower, upper, ok := semverWindow(clause.Value, true)
		if !ok {
			return []types.VersionConstraint{clause}, nil
		}
		return []types.VersionConstraint{{Operator: types.OpGte, Value: lower}, {Operator: types.OpLt, Value: upper}}, nil
	case types.OpWeakGt:
		_, upper, ok := semverWindow(clause.Value, false)
		if !ok {
			return []types.VersionConstraint{{Operator: types.OpGt, Value: clause.Value}}, nil
		}
		return []types.VersionConstraint{{Operator: types.OpGt, Value: clause.Value}, {Operator: types.OpLt, Value: upper}}, nil
	default:
		return []types.VersionConstraint{clause}, nil
	}
}

type semverTuple interface {
	Major() uint64
	Minor() uint64
	Patch() uint64
}

func semverWindow(value types.ComparableVersion, tilde bool) (types.ComparableVersion, types.ComparableVersion, bool) {
	if value == nil || value.Scheme() != types.Semver {
		return nil, nil, false
	}
	sv, ok := value.(semverTuple)
	if !ok {
		return nil, nil, false
	}
	if tilde {
		if sv.Minor() == 0 && sv.Patch() == 0 {
			return value, dependency.NewSemver(sv.Major()+1, 0, 0), true
		}
		return value, dependency.NewSemver(sv.Major(), sv.Minor()+1, 0), true
	}
	return value, dependency.NewSemver(sv.Major()+1, 0, 0), true
}

func variantsToExpression(variants []constraintVariant) types.VersionConstraintExpression {
	expr := make(types.VersionConstraintExpression, 0, len(variants))
	for _, variant := range variants {
		group := make([]types.VersionConstraint, 0, len(variant.Clauses))
		for _, clause := range variant.Clauses {
			group = append(group, clause.Constraint)
		}
		expr = append(expr, group)
	}
	if len(expr) == 0 {
		return types.VersionConstraintExpression{{}}
	}
	return expr
}

func flattenProvenance(variants []constraintVariant) []ConstraintProvenance {
	provenance := make([]ConstraintProvenance, 0)
	for _, variant := range variants {
		provenance = append(provenance, variant.Clauses...)
	}
	return provenance
}

func cloneConstraintExpression(expr types.VersionConstraintExpression) types.VersionConstraintExpression {
	cloned := make(types.VersionConstraintExpression, len(expr))
	for i, group := range expr {
		cloned[i] = append([]types.VersionConstraint(nil), group...)
	}
	return cloned
}

func conflictFor(id types.PackageId, left, right ConstraintProvenance) *ConstraintConflictError {
	return &ConstraintConflictError{
		PackageId: id,
		Left:      ConstraintConflictSource{Requester: left.Requester, Constraint: left.Constraint},
		Right:     ConstraintConflictSource{Requester: right.Requester, Constraint: right.Constraint},
	}
}

func formatVersionConstraint(constraint types.VersionConstraint) string {
	return constraint.Operator.ToSign() + fmt.Sprint(constraint.Value)
}
