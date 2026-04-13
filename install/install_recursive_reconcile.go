package install

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mclucy/lucy/types"
)

// ReconcileTransaction compares advisory candidate facts with authoritative
// verified facts, computes a stable diff, and validates tightened local
// constraints through the merge engine.
func ReconcileTransaction(tx *RecursiveTransaction) (ReconcileDiff, error) {
	if tx == nil {
		return ReconcileDiff{}, fmt.Errorf("install: nil recursive transaction")
	}
	if tx.Phase != PhaseVerified {
		return ReconcileDiff{}, fmt.Errorf("install: reconcile requires PhaseVerified transaction")
	}

	showRecursiveReconcileStart()

	baseInputs, err := reconcileConstraintInputs(tx)
	if err != nil {
		return ReconcileDiff{}, err
	}

	var previousDiff ReconcileDiff
	havePrevious := false

	for {
		diff, diffErr := reconcileDiff(tx)
		if diffErr != nil {
			return ReconcileDiff{}, diffErr
		}
		tx.ReconcileDiff = diff
		showRecursiveReconcileDiff(diff)

		if diff.IsStable() {
			return diff, nil
		}

		inputs := append([]ConstraintInput(nil), baseInputs...)
		inputs = append(inputs, diff.Tightened...)
		_, mergeErr := MergeConstraintGraph(inputs)

		if havePrevious && reconcileProgressStalled(previousDiff, diff) {
			if mergeErr != nil {
				return ReconcileDiff{}, fmt.Errorf("install: reconcile made no progress, aborting")
			}
			return diff, nil
		}

		previousDiff = diff
		havePrevious = true
	}
}

func reconcileDiff(tx *RecursiveTransaction) (ReconcileDiff, error) {
	missing := make(map[string]types.PackageId)
	tightened := make(map[string]ConstraintInput)

	for key, verifiedNode := range tx.VerifiedGraph {
		verifiedDeps, err := reconcileDependencyMap(verifiedNode.Package.Id.StringFull(), verifiedNode.Package.Dependencies)
		if err != nil {
			return ReconcileDiff{}, err
		}

		advisoryDeps := map[string]types.Dependency{}
		if advisoryNode, ok := tx.CandidateGraph[key]; ok {
			advisoryDeps, err = reconcileDependencyMap(advisoryNode.Package.Id.StringFull(), advisoryNode.Package.Dependencies)
			if err != nil {
				return ReconcileDiff{}, err
			}
		}

		for depKey, verifiedDep := range verifiedDeps {
			if verifiedDep.Mandatory {
				if _, exists := tx.CandidateGraph[depKey]; !exists {
					missing[depKey] = verifiedDep.Id
				}
			}

			advisoryDep, ok := advisoryDeps[depKey]
			if !ok {
				continue
			}

			if !reconcileConstraintTightened(advisoryDep, verifiedDep) {
				continue
			}

			tightened[reconcileTightenedKey(verifiedNode.Package.Id.StringFull(), depKey)] = ConstraintInput{
				Requester:  verifiedNode.Package.Id.StringFull(),
				Dependency: verifiedDep,
			}
		}
	}

	reachable, err := reconcileReachableCandidateClosure(tx)
	if err != nil {
		return ReconcileDiff{}, err
	}

	// Build a name-only index of verified nodes to handle platform normalisation:
	// upstream APIs may return platform=none/any/unknown for a package that the
	// local detector identifies as forge/fabric/etc. A candidate keyed as
	// "none/create" is the same artifact as a verified node keyed "forge/create".
	verifiedByName := make(map[types.ProjectName]struct{}, len(tx.VerifiedGraph))
	for _, vn := range tx.VerifiedGraph {
		verifiedByName[vn.Package.Id.Name] = struct{}{}
	}

	extra := make(map[string]types.PackageId)
	for key, candidateNode := range tx.CandidateGraph {
		if !candidateNode.Advisory {
			continue
		}
		if _, ok := reachable[key]; ok {
			continue
		}
		// Treat a platform-wildcard candidate as reachable if a verified node
		// with the same name exists — they represent the same artifact.
		if candidateNode.Package.Id.Platform.CanInfer() {
			if _, ok := verifiedByName[candidateNode.Package.Id.Name]; ok {
				continue
			}
		}
		extra[key] = candidateNode.Package.Id
	}

	return ReconcileDiff{
		Missing:   reconcileSortedPackageIDs(missing),
		Extra:     reconcileSortedPackageIDs(extra),
		Tightened: reconcileSortedConstraintInputs(tightened),
	}, nil
}

func reconcileConstraintInputs(tx *RecursiveTransaction) ([]ConstraintInput, error) {
	inputs := make([]ConstraintInput, 0)

	for _, root := range tx.Roots {
		inputs = append(inputs, ConstraintInput{
			Requester: "root",
			Dependency: types.Dependency{
				Id:        root,
				Mandatory: true,
			},
		})
	}

	for _, installed := range tx.InstalledConstraints {
		inputs = append(inputs, installed.ConstraintInput)
	}

	keys := make(map[string]struct{}, len(tx.CandidateGraph)+len(tx.VerifiedGraph))
	for key := range tx.CandidateGraph {
		keys[key] = struct{}{}
	}
	for key := range tx.VerifiedGraph {
		keys[key] = struct{}{}
	}

	orderedKeys := make([]string, 0, len(keys))
	for key := range keys {
		orderedKeys = append(orderedKeys, key)
	}
	slices.Sort(orderedKeys)

	for _, key := range orderedKeys {
		node, ok := tx.VerifiedGraph[key]
		if !ok {
			node, ok = tx.CandidateGraph[key]
		}
		if !ok {
			continue
		}

		deps, err := reconcileDependencyMap(node.Package.Id.StringFull(), node.Package.Dependencies)
		if err != nil {
			return nil, err
		}

		depKeys := make([]string, 0, len(deps))
		for depKey := range deps {
			depKeys = append(depKeys, depKey)
		}
		slices.Sort(depKeys)

		for _, depKey := range depKeys {
			inputs = append(inputs, ConstraintInput{
				Requester:  node.Package.Id.StringFull(),
				Dependency: deps[depKey],
			})
		}
	}

	return inputs, nil
}

func reconcileDependencyMap(requester string, deps *types.PackageDependencies) (map[string]types.Dependency, error) {
	if deps == nil || len(deps.Value) == 0 {
		return map[string]types.Dependency{}, nil
	}

	mandatory := make(map[string]bool)
	inputs := make([]ConstraintInput, 0, len(deps.Value))
	for _, dep := range deps.Value {
		key := dep.Id.StringPlatformName()
		mandatory[key] = mandatory[key] || dep.Mandatory
		inputs = append(inputs, ConstraintInput{Requester: requester, Dependency: dep})
	}

	graph, err := MergeConstraintGraph(inputs)
	if err != nil {
		return nil, err
	}

	merged := make(map[string]types.Dependency, len(graph))
	for key, requirement := range graph {
		merged[key] = types.Dependency{
			Id: types.PackageId{
				Platform: requirement.Id.Platform,
				Name:     requirement.Id.Name,
			},
			Constraint: requirement.Constraint,
			Mandatory:  mandatory[key],
		}
	}

	return merged, nil
}

func reconcileReachableCandidateClosure(tx *RecursiveTransaction) (map[string]struct{}, error) {
	reachable := make(map[string]struct{}, len(tx.VerifiedGraph))
	queue := make([]string, 0, len(tx.VerifiedGraph))

	for key := range tx.VerifiedGraph {
		reachable[key] = struct{}{}
		queue = append(queue, key)
	}

	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]

		node, ok := tx.VerifiedGraph[key]
		if !ok {
			node, ok = tx.CandidateGraph[key]
		}
		if !ok {
			continue
		}

		deps, err := reconcileDependencyMap(node.Package.Id.StringFull(), node.Package.Dependencies)
		if err != nil {
			return nil, err
		}

		depKeys := make([]string, 0, len(deps))
		for depKey := range deps {
			depKeys = append(depKeys, depKey)
		}
		slices.Sort(depKeys)

		for _, depKey := range depKeys {
			if _, exists := tx.CandidateGraph[depKey]; !exists {
				continue
			}
			if _, seen := reachable[depKey]; seen {
				continue
			}
			reachable[depKey] = struct{}{}
			queue = append(queue, depKey)
		}
	}

	return reachable, nil
}

func reconcileConstraintTightened(advisory, verified types.Dependency) bool {
	if verified.Mandatory && !advisory.Mandatory {
		return true
	}

	if advisory.Mandatory != verified.Mandatory && !verified.Mandatory {
		return false
	}

	if reconcileConstraintExpressionKey(advisory.Constraint) == reconcileConstraintExpressionKey(verified.Constraint) {
		return false
	}

	merged, err := MergeConstraintGraph([]ConstraintInput{
		{Requester: "advisory", Dependency: advisory},
		{Requester: "verified", Dependency: verified},
	})
	if err != nil {
		return true
	}

	entry, ok := merged[verified.Id.StringPlatformName()]
	if !ok {
		return false
	}

	return reconcileConstraintExpressionKey(entry.Constraint) == reconcileConstraintExpressionKey(verified.Constraint)
}

func reconcileProgressStalled(previous, current ReconcileDiff) bool {
	return reconcilePackageIDSetKey(previous.Missing) == reconcilePackageIDSetKey(current.Missing) &&
		reconcileTightenedSetKey(previous.Tightened) == reconcileTightenedSetKey(current.Tightened)
}

func reconcileSortedPackageIDs(items map[string]types.PackageId) []types.PackageId {
	result := make([]types.PackageId, 0, len(items))
	for _, id := range items {
		result = append(result, id)
	}

	slices.SortFunc(result, func(a, b types.PackageId) int {
		if a.Platform != b.Platform {
			return strings.Compare(a.Platform.String(), b.Platform.String())
		}
		if a.Name != b.Name {
			return strings.Compare(a.Name.String(), b.Name.String())
		}
		return strings.Compare(a.Version.String(), b.Version.String())
	})

	return result
}

func reconcileSortedConstraintInputs(items map[string]ConstraintInput) []ConstraintInput {
	result := make([]ConstraintInput, 0, len(items))
	for _, input := range items {
		result = append(result, input)
	}

	slices.SortFunc(result, func(a, b ConstraintInput) int {
		if a.Requester != b.Requester {
			return strings.Compare(a.Requester, b.Requester)
		}
		if a.Dependency.Id.Platform != b.Dependency.Id.Platform {
			return strings.Compare(a.Dependency.Id.Platform.String(), b.Dependency.Id.Platform.String())
		}
		if a.Dependency.Id.Name != b.Dependency.Id.Name {
			return strings.Compare(a.Dependency.Id.Name.String(), b.Dependency.Id.Name.String())
		}
		return strings.Compare(
			reconcileConstraintExpressionKey(a.Dependency.Constraint),
			reconcileConstraintExpressionKey(b.Dependency.Constraint),
		)
	})

	return result
}

func reconcilePackageIDSetKey(ids []types.PackageId) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, id.StringFull())
	}
	slices.Sort(parts)
	return strings.Join(parts, "|")
}

func reconcileTightenedSetKey(inputs []ConstraintInput) string {
	parts := make([]string, 0, len(inputs))
	for _, input := range inputs {
		parts = append(parts, reconcileTightenedKey(input.Requester, input.Dependency.Id.StringPlatformName())+"="+reconcileConstraintExpressionKey(input.Dependency.Constraint))
	}
	slices.Sort(parts)
	return strings.Join(parts, "|")
}

func reconcileTightenedKey(requester, depKey string) string {
	return requester + "->" + depKey
}

func reconcileConstraintExpressionKey(expr types.VersionConstraintExpression) string {
	if len(expr) == 0 {
		return "any"
	}

	groups := make([]string, 0, len(expr))
	for _, group := range expr {
		if len(group) == 0 {
			groups = append(groups, "any")
			continue
		}

		clauses := make([]string, 0, len(group))
		for _, clause := range group {
			clauses = append(clauses, formatVersionConstraint(clause))
		}
		slices.Sort(clauses)
		groups = append(groups, strings.Join(clauses, "&"))
	}

	slices.Sort(groups)
	return strings.Join(groups, "|")
}
