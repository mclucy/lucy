package install

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/types"
)

// recursiveResolutionPlan is the pure value contract for one recursive
// resolution pass: which roots to solve, which fixed constraints to respect,
// and which advisory candidates must be pruned before verification.
type recursiveResolutionPlan struct {
	Roots                []types.VersionedPackageRef
	InstalledConstraints []InstalledConstraint
	ExcludedCandidates   map[string]struct{}
}

func newRecursiveResolutionPlan(
	roots []types.VersionedPackageRef,
	installedConstraints []InstalledConstraint,
) recursiveResolutionPlan {
	return recursiveResolutionPlan{
		Roots: append(
			[]types.VersionedPackageRef(nil),
			roots...,
		),
		InstalledConstraints: append(
			[]InstalledConstraint(nil),
			installedConstraints...,
		),
		ExcludedCandidates: map[string]struct{}{},
	}
}

func refineRecursiveResolutionPlan(
	plan recursiveResolutionPlan,
	diff ReconcileDiff,
) recursiveResolutionPlan {
	return recursiveResolutionPlan{
		Roots: appendMissingRoots(plan.Roots, diff.Missing),
		InstalledConstraints: mergeReconcileConstraints(
			plan.InstalledConstraints,
			tightenedConstraintInputs(diff.Tightened),
		),
		ExcludedCandidates: excludedCandidateKeys(diff.Extra),
	}
}

func summarizeReconcileDiff(diff ReconcileDiff) string {
	parts := make([]string, 0, 3)
	if len(diff.Missing) > 0 {
		parts = append(parts, fmt.Sprintf("missing=%d", len(diff.Missing)))
	}
	if len(diff.Extra) > 0 {
		parts = append(parts, fmt.Sprintf("extra=%d", len(diff.Extra)))
	}
	if len(diff.Tightened) > 0 {
		parts = append(parts, fmt.Sprintf("tightened=%d", len(diff.Tightened)))
	}
	if len(parts) == 0 {
		return "no changes"
	}
	return strings.Join(parts, ", ")
}

func excludedCandidateKeys(ids []types.VersionedPackageRef) map[string]struct{} {
	excluded := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		excluded[id.StringPlatformName()] = struct{}{}
	}
	return excluded
}

func appendMissingRoots(
	existing []types.VersionedPackageRef,
	missing []types.VersionedPackageRef,
) []types.VersionedPackageRef {
	if len(missing) == 0 {
		return append([]types.VersionedPackageRef(nil), existing...)
	}

	seen := make(map[string]struct{}, len(existing)+len(missing))
	updated := make([]types.VersionedPackageRef, 0, len(existing)+len(missing))
	for _, id := range existing {
		key := id.StringPlatformName()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		updated = append(updated, id)
	}
	for _, id := range missing {
		key := id.StringPlatformName()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		updated = append(updated, id)
	}

	return updated
}

func mergeReconcileConstraints(groups ...[]InstalledConstraint) []InstalledConstraint {
	merged := make([]InstalledConstraint, 0)
	index := make(map[string]int)

	for _, group := range groups {
		for _, constraint := range group {
			key := reconcileConstraintInputKey(constraint.ConstraintInput)
			if pos, ok := index[key]; ok {
				merged[pos] = constraint
				continue
			}
			index[key] = len(merged)
			merged = append(merged, constraint)
		}
	}

	return merged
}

func tightenedConstraintInputs(inputs []ConstraintInput) []InstalledConstraint {
	constraints := make([]InstalledConstraint, 0, len(inputs))
	for _, input := range inputs {
		constraints = append(
			constraints,
			InstalledConstraint{ConstraintInput: input},
		)
	}
	return constraints
}

func reconcileConstraintInputKey(input ConstraintInput) string {
	return input.Requester + "|" + input.Dependency.Id.StringPlatformName()
}
