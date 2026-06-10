package install

import (
	"fmt"

	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/types"
)

func constraintInputVariants(input ConstraintInput) (
	[]constraintVariant,
	error,
) {
	expr, err := normalizeConstraintExpression(input.Dependency)
	if err != nil {
		return nil, err
	}
	variants := make([]constraintVariant, 0, len(expr))
	for _, group := range expr {
		variant := constraintVariant{
			Clauses: make(
				[]ConstraintProvenance,
				0,
				len(group),
			),
		}
		for _, clause := range group {
			expanded, expandErr := expandConstraint(clause)
			if expandErr != nil {
				return nil, expandErr
			}
			for _, expandedClause := range expanded {
				variant.Clauses = append(
					variant.Clauses, ConstraintProvenance{
						Requester:  input.Requester,
						Constraint: expandedClause,
					},
				)
			}
		}
		variants = append(variants, variant)
	}
	if len(variants) == 0 {
		return []constraintVariant{{}}, nil
	}
	return variants, nil
}

func normalizeConstraintExpression(dep types.Dependency) (
	types.VersionExpr,
	error,
) {
	if len(dep.Constraint) > 0 {
		return cloneConstraintExpression(dep.Constraint), nil
	}
	if dep.Id.Version == "" || dep.Id.Version == types.VersionAny || dep.Id.Version.IsInvalid() || dep.Id.Version.CanInfer() {
		return types.VersionExpr{{}}, nil
	}
	value, err := dependency.Parse(dep.Id.Version, defaultVersionScheme(dep.Id))
	if err != nil {
		return nil, fmt.Errorf(
			"install: failed to parse fixed constraint version %q for %s: %w",
			dep.Id.Version,
			dep.Id.StringBase(),
			err,
		)
	}
	if value == nil {
		return nil, fmt.Errorf(
			"install: failed to parse fixed constraint version %q for %s",
			dep.Id.Version,
			dep.Id.StringBase(),
		)
	}
	return types.VersionExpr{
		{
			{
				Operator: types.OpEq,
				Value:    value,
			},
		},
	}, nil
}

func defaultVersionScheme(id types.VersionedPackageRef) types.VersionScheme {
	switch id.Platform {
	case types.PlatformMinecraft:
		return types.MinecraftRelease
	case types.PlatformForge, types.PlatformNeoforge:
		return types.Maven
	default:
		return types.Semver
	}
}

func expandConstraint(clause types.VersionSubExpr) (
	[]types.VersionSubExpr,
	error,
) {
	switch clause.Operator {
	case types.OpWeakEq:
		lower, upper, ok := semverWindow(clause.Value, true)
		if !ok {
			return []types.VersionSubExpr{clause}, nil
		}
		return []types.VersionSubExpr{
			{Operator: types.OpGte, Value: lower},
			{Operator: types.OpLt, Value: upper},
		}, nil
	case types.OpWeakGt:
		_, upper, ok := semverWindow(clause.Value, false)
		if !ok {
			return []types.VersionSubExpr{
				{
					Operator: types.OpGt, Value: clause.Value,
				},
			}, nil
		}
		return []types.VersionSubExpr{
			{
				Operator: types.OpGt, Value: clause.Value,
			}, {Operator: types.OpLt, Value: upper},
		}, nil
	default:
		return []types.VersionSubExpr{clause}, nil
	}
}

type semverTuple interface {
	Major() uint64
	Minor() uint64
	Patch() uint64
}

func semverWindow(
	value types.ResolvableVersion,
	tilde bool,
) (types.ResolvableVersion, types.ResolvableVersion, bool) {
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

func variantsToExpression(variants []constraintVariant) types.VersionExpr {
	expr := make(types.VersionExpr, 0, len(variants))
	for _, variant := range variants {
		group := make([]types.VersionSubExpr, 0, len(variant.Clauses))
		for _, clause := range variant.Clauses {
			group = append(group, clause.Constraint)
		}
		expr = append(expr, group)
	}
	if len(expr) == 0 {
		return types.VersionExpr{{}}
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

func cloneConstraintExpression(expr types.VersionExpr) types.VersionExpr {
	cloned := make(types.VersionExpr, len(expr))
	for i, group := range expr {
		cloned[i] = append([]types.VersionSubExpr(nil), group...)
	}
	return cloned
}
