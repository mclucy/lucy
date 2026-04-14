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

	diff, err := reconcileDiffKernel(
		tx.Roots,
		tx.InstalledConstraints,
		tx.CandidateGraph,
		tx.VerifiedGraph,
	)
	if err != nil {
		return ReconcileDiff{}, err
	}

	tx.ReconcileDiff = diff
	showRecursiveReconcileDiff(diff)

	return diff, nil
}

func reconcileDiffKernel(
	roots []types.PackageId,
	installed []InstalledConstraint,
	candidateGraph map[string]CandidateNode,
	verifiedGraph map[string]CandidateNode,
) (ReconcileDiff, error) {
	baseInputs, err := reconcileConstraintInputs(roots, installed, candidateGraph, verifiedGraph)
	if err != nil {
		return ReconcileDiff{}, err
	}

	diff, err := reconcileDiff(candidateGraph, verifiedGraph)
	if err != nil {
		return ReconcileDiff{}, err
	}

	if diff.IsStable() {
		return diff, nil
	}

	if err := reconcileValidateTightenedDiff(baseInputs, diff); err != nil {
		return ReconcileDiff{}, err
	}

	return diff, nil
}

func reconcileDiff(candidateGraph map[string]CandidateNode, verifiedGraph map[string]CandidateNode) (ReconcileDiff, error) {
	missing := make(map[string]types.PackageId)
	tightened := make(map[string]ConstraintInput)

	for key, verifiedNode := range verifiedGraph {
		verifiedDeps, err := reconcileDependencyMap(verifiedNode.Package.Id.StringFull(), verifiedNode.Package.Dependencies)
		if err != nil {
			return ReconcileDiff{}, err
		}

		advisoryDeps := map[string]types.Dependency{}
		if advisoryNode, ok := candidateGraph[key]; ok {
			advisoryDeps, err = reconcileDependencyMap(advisoryNode.Package.Id.StringFull(), advisoryNode.Package.Dependencies)
			if err != nil {
				return ReconcileDiff{}, err
			}
		}

		for depKey, verifiedDep := range verifiedDeps {
			// Embedded deps are physically bundled inside the parent JAR
			// (e.g. NeoForge JarInJar). They are already present on disk and
			// do not need to be resolved from upstream package registries.
			if verifiedDep.Mandatory && !verifiedDep.Embedded {
				if _, exists := candidateGraph[depKey]; !exists {
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

	reachable, err := reconcileReachableCandidateClosure(candidateGraph, verifiedGraph)
	if err != nil {
		return ReconcileDiff{}, err
	}

	// Build a name-only index of verified nodes to handle platform normalisation:
	// upstream APIs may return platform=none/any/unknown for a package that the
	// local detector identifies as forge/fabric/etc. A candidate keyed as
	// "none/create" is the same artifact as a verified node keyed "forge/create".
	verifiedByName := make(map[types.ProjectName]struct{}, len(verifiedGraph))
	for _, vn := range verifiedGraph {
		verifiedByName[vn.Package.Id.Name] = struct{}{}
	}

	extra := make(map[string]types.PackageId)
	for key, candidateNode := range candidateGraph {
		if !candidateNode.Advisory {
			continue
		}
		if _, ok := reachable[key]; ok {
			continue
		}
		// Treat a platform-wildcard candidate as reachable if a verified node
		// with the same name exists — they represent the same artifact.
		p := candidateNode.Package.Id.Platform
		if p == types.PlatformNone || p == types.PlatformAny || p.CanInfer() {
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

func reconcileValidateTightenedDiff(baseInputs []ConstraintInput, diff ReconcileDiff) error {
	if len(diff.Tightened) == 0 {
		return nil
	}

	inputs := append([]ConstraintInput(nil), baseInputs...)
	inputs = append(inputs, diff.Tightened...)
	if _, err := MergeConstraintGraph(inputs); err != nil {
		return fmt.Errorf("install: reconcile made no progress, aborting")
	}

	return nil
}

func reconcileConstraintInputs(
	roots []types.PackageId,
	installed []InstalledConstraint,
	candidateGraph map[string]CandidateNode,
	verifiedGraph map[string]CandidateNode,
) ([]ConstraintInput, error) {
	inputs := make([]ConstraintInput, 0)

	for _, root := range roots {
		inputs = append(inputs, ConstraintInput{
			Requester: "root",
			Dependency: types.Dependency{
				Id:        root,
				Mandatory: true,
			},
		})
	}

	for _, installed := range installed {
		inputs = append(inputs, installed.ConstraintInput)
	}

	keys := make(map[string]struct{}, len(candidateGraph)+len(verifiedGraph))
	for key := range candidateGraph {
		keys[key] = struct{}{}
	}
	for key := range verifiedGraph {
		keys[key] = struct{}{}
	}

	orderedKeys := make([]string, 0, len(keys))
	for key := range keys {
		orderedKeys = append(orderedKeys, key)
	}
	slices.Sort(orderedKeys)

	for _, key := range orderedKeys {
		node, ok := verifiedGraph[key]
		if !ok {
			node, ok = candidateGraph[key]
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
	embedded := make(map[string]bool)
	inputs := make([]ConstraintInput, 0, len(deps.Value))
	for _, dep := range deps.Value {
		key := dep.Id.StringPlatformName()
		mandatory[key] = mandatory[key] || dep.Mandatory
		embedded[key] = embedded[key] || dep.Embedded
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
			Embedded:   embedded[key],
		}
	}

	return merged, nil
}

func reconcileReachableCandidateClosure(
	candidateGraph map[string]CandidateNode,
	verifiedGraph map[string]CandidateNode,
) (map[string]struct{}, error) {
	reachable := make(map[string]struct{}, len(verifiedGraph))
	queue := make([]string, 0, len(verifiedGraph))

	for key := range verifiedGraph {
		reachable[key] = struct{}{}
		queue = append(queue, key)
	}

	for len(queue) > 0 {
		key := queue[0]
		queue = queue[1:]

		node, ok := verifiedGraph[key]
		if !ok {
			node, ok = candidateGraph[key]
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
			if _, exists := candidateGraph[depKey]; !exists {
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
