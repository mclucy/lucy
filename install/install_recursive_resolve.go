package install

import (
	"strings"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
)

type candidateRequest struct {
	id             types.PackageId
	provenancePath []string
	mandatory      bool
}

type candidateGraphResolver interface {
	ResolvePackage(id types.PackageId) (types.Package, error)
	ResolveDependencies(pkg types.Package) ([]types.PackageDependencies, error)
}

type candidateGraphPlanner struct {
	tx               *RecursiveTransaction
	constraintInputs []ConstraintInput
	queue            []candidateRequest
}

// BuildCandidateGraph expands the recursive advisory dependency closure for the
// requested roots, seeding fixed installed constraints up front and running the
// constraint merge engine after every newly discovered dependency batch.
func BuildCandidateGraph(
	roots []types.PackageId,
	providers []upstream.Provider,
	installedConstraints []InstalledConstraint,
	options Options,
) (*RecursiveTransaction, error) {
	return BuildCandidateGraphWithResolver(
		roots,
		providers,
		installedConstraints,
		options,
		providerCandidateResolver{providers: providers},
	)
}

// BuildCandidateGraphWithResolver drives candidate-graph expansion using a
// caller-provided resolver so the planning loop can run without direct provider
// or routing calls in the planner core.
func BuildCandidateGraphWithResolver(
	roots []types.PackageId,
	providers []upstream.Provider,
	installedConstraints []InstalledConstraint,
	options Options,
	resolver candidateGraphResolver,
) (*RecursiveTransaction, error) {
	planner, err := newCandidateGraphPlanner(roots, providers, installedConstraints)
	if err != nil {
		return nil, err
	}

	for {
		current, ok := planner.next()
		if !ok {
			return planner.transaction(), nil
		}

		pkg, err := resolver.ResolvePackage(current.id)
		if err != nil {
			if current.mandatory {
				return nil, err
			}
			continue
		}

		dependencySets, err := resolver.ResolveDependencies(pkg)
		if err != nil {
			if current.mandatory {
				return nil, err
			}
			continue
		}

		if err := planner.admit(current, pkg, dependencySets, options); err != nil {
			return nil, err
		}
	}
}

func newCandidateGraphPlanner(
	roots []types.PackageId,
	providers []upstream.Provider,
	installedConstraints []InstalledConstraint,
) (*candidateGraphPlanner, error) {
	tx := NewRecursiveTransaction(roots, providers)
	tx.InstalledConstraints = append([]InstalledConstraint(nil), installedConstraints...)

	constraintInputs := make([]ConstraintInput, 0, len(installedConstraints))
	for _, installed := range installedConstraints {
		constraintInputs = append(constraintInputs, installed.ConstraintInput)
		if installed.Package.Id.Platform == "" || installed.Package.Id.Name == "" {
			continue
		}
		key := installed.Package.Id.StringPlatformName()
		if _, exists := tx.CandidateGraph[key]; exists {
			continue
		}
		tx.CandidateGraph[key] = CandidateNode{
			Package:        installed.Package,
			ProvenancePath: []string{installed.ConstraintInput.Requester},
			Advisory:       false,
		}
	}

	if _, err := MergeConstraintGraph(constraintInputs); err != nil {
		return nil, err
	}

	queue := make([]candidateRequest, 0, len(roots))
	for _, root := range roots {
		ReportCompatibleInstalled(tx, root)
		queue = append(queue, candidateRequest{
			id:             root,
			provenancePath: []string{"root"},
			mandatory:      true,
		})
	}

	return &candidateGraphPlanner{
		tx:               tx,
		constraintInputs: constraintInputs,
		queue:            queue,
	}, nil
}

func (planner *candidateGraphPlanner) next() (candidateRequest, bool) {
	for len(planner.queue) > 0 {
		current := planner.queue[0]
		planner.queue = planner.queue[1:]

		key := current.id.StringPlatformName()
		if _, exists := planner.tx.CandidateGraph[key]; exists {
			continue
		}

		return current, true
	}

	return candidateRequest{}, false
}

func (planner *candidateGraphPlanner) admit(
	current candidateRequest,
	pkg types.Package,
	dependencySets []types.PackageDependencies,
	options Options,
) error {
	key := current.id.StringPlatformName()
	planner.tx.CandidateGraph[key] = CandidateNode{
		Package:        pkg,
		ProvenancePath: append([]string(nil), current.provenancePath...),
		Advisory:       true,
	}

	batchInputs := make([]ConstraintInput, 0)
	children := make([]candidateRequest, 0)
	for _, dependencySet := range dependencySets {
		requester := current.id.StringFull()
		for _, dependency := range dependencySet.Value {
			if !dependency.Mandatory && !options.WithOptional {
				continue
			}

			batchInputs = append(batchInputs, ConstraintInput{
				Requester:  requester,
				Dependency: dependency,
			})

			childKey := dependency.Id.StringPlatformName()
			if _, exists := planner.tx.CandidateGraph[childKey]; exists {
				continue
			}

			children = append(children, candidateRequest{
				id:             dependency.Id,
				provenancePath: appendPath(current.provenancePath, requester),
				mandatory:      dependency.Mandatory,
			})
		}
	}

	if len(batchInputs) > 0 {
		planner.constraintInputs = append(planner.constraintInputs, batchInputs...)
		if _, err := MergeConstraintGraph(planner.constraintInputs); err != nil {
			return err
		}
	}

	planner.queue = append(planner.queue, children...)
	return nil
}

func (planner *candidateGraphPlanner) transaction() *RecursiveTransaction {
	if planner == nil {
		return nil
	}
	return planner.tx
}

func appendPath(path []string, requester string) []string {
	next := make([]string, 0, len(path)+1)
	next = append(next, path...)
	next = append(next, requester)
	return next
}

func formatProviderErrors(providerErrors []routing.ProviderError) string {
	if len(providerErrors) == 0 {
		return "no provider succeeded"
	}

	reasons := make([]string, 0, len(providerErrors))
	for _, providerErr := range providerErrors {
		reasons = append(reasons, providerErr.Error())
	}
	return strings.Join(reasons, "; ")
}
