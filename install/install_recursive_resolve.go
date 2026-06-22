package install

import (
	"context"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
)

type candidateRequest struct {
	id             types.VersionedPackageRef
	provenancePath []string
	mandatory      bool
}

type candidateGraphResolver interface {
	ResolvePackage(ctx context.Context, id types.VersionedPackageRef) (types.Package, error)
	ResolveDependencies(ctx context.Context, pkg types.Package) ([]types.PackageDependencies, error)
}

type candidateGraphPlanner struct {
	roots                []types.VersionedPackageRef
	providers            []upstream.PackageSource
	installedConstraints []InstalledConstraint
	candidateGraph       map[string]CandidateNode
	constraintInputs     []resolve.ConstraintInput
	queue                []candidateRequest
}

// BuildCandidateGraph expands the recursive advisory dependency closure for the
// requested roots, seeding fixed installed constraints up front and running the
// constraint merge engine after every newly discovered dependency batch.
func BuildCandidateGraph(
	ctx context.Context,
	roots []types.VersionedPackageRef,
	providers []upstream.PackageSource,
	installedConstraints []InstalledConstraint,
	options InstallOptions,
) (ResolvedClosure, error) {
	return resolveClosure(
		ctx,
		roots,
		providers,
		installedConstraints,
		options,
		providerCandidateResolver{providers: providers},
	)
}

func resolveClosure(
	ctx context.Context,
	roots []types.VersionedPackageRef,
	providers []upstream.PackageSource,
	installed []InstalledConstraint,
	options InstallOptions,
	resolver candidateGraphResolver,
) (ResolvedClosure, error) {
	return BuildCandidateGraphWithResolver(
		ctx,
		roots,
		providers,
		installed,
		options,
		resolver,
	)
}

// BuildCandidateGraphWithResolver drives candidate-graph expansion using a
// caller-provided resolver so the planning loop can run without direct provider
// or routing calls in the planner core.
func BuildCandidateGraphWithResolver(
	ctx context.Context,
	roots []types.VersionedPackageRef,
	providers []upstream.PackageSource,
	installedConstraints []InstalledConstraint,
	options InstallOptions,
	resolver candidateGraphResolver,
) (ResolvedClosure, error) {
	options = options.withDefaults()
	planner, err := newCandidateGraphPlanner(
		roots,
		providers,
		installedConstraints,
	)
	if err != nil {
		return ResolvedClosure{}, err
	}

	for {
		if err := ctx.Err(); err != nil {
			return ResolvedClosure{}, err
		}

		current, ok := planner.next()
		if !ok {
			return planner.resolvedClosure(), nil
		}

		pkg, err := resolver.ResolvePackage(ctx, current.id)
		if err != nil {
			if current.mandatory {
				return ResolvedClosure{}, err
			}
			continue
		}

		dependencySets, err := resolver.ResolveDependencies(ctx, pkg)
		if err != nil {
			if current.mandatory {
				return ResolvedClosure{}, err
			}
			continue
		}

		if err := planner.admit(
			current,
			pkg,
			dependencySets,
			options,
		); err != nil {
			return ResolvedClosure{}, err
		}
	}
}

func newCandidateGraphPlanner(
	roots []types.VersionedPackageRef,
	providers []upstream.PackageSource,
	installedConstraints []InstalledConstraint,
) (*candidateGraphPlanner, error) {
	candidateGraph := make(map[string]CandidateNode)
	installedConstraints = append(
		[]InstalledConstraint(nil),
		installedConstraints...,
	)

	constraintInputs := make([]resolve.ConstraintInput, 0, len(installedConstraints))
	for _, installed := range installedConstraints {
		constraintInputs = append(constraintInputs, installed.ConstraintInput)
		if installed.Package.Id.Platform == "" || installed.Package.Id.Name == "" {
			continue
		}
		key := installed.Package.Id.StringBase()
		if _, exists := candidateGraph[key]; exists {
			continue
		}
		candidateGraph[key] = CandidateNode{
			Package:        installed.Package,
			ProvenancePath: []string{installed.ConstraintInput.Requester},
			Advisory:       false,
		}
	}

	if _, err := resolve.MergeConstraintGraph(constraintInputs); err != nil {
		return nil, err
	}

	queue := make([]candidateRequest, 0, len(roots))
	for _, root := range roots {
		ReportCompatibleInstalled(installedConstraints, root)
		queue = append(
			queue, candidateRequest{
				id:             root,
				provenancePath: []string{"root"},
				mandatory:      true,
			},
		)
	}

	return &candidateGraphPlanner{
		roots:                append([]types.VersionedPackageRef(nil), roots...),
		providers:            append([]upstream.PackageSource(nil), providers...),
		installedConstraints: installedConstraints,
		candidateGraph:       candidateGraph,
		constraintInputs:     constraintInputs,
		queue:                queue,
	}, nil
}

func (planner *candidateGraphPlanner) next() (candidateRequest, bool) {
	for len(planner.queue) > 0 {
		current := planner.queue[0]
		planner.queue = planner.queue[1:]

		key := current.id.StringBase()
		if _, exists := planner.candidateGraph[key]; exists {
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
	options InstallOptions,
) error {
	key := current.id.StringBase()
	planner.candidateGraph[key] = CandidateNode{
		Package:        pkg,
		ProvenancePath: append([]string(nil), current.provenancePath...),
		Advisory:       true,
	}

	batchInputs := make([]resolve.ConstraintInput, 0)
	children := make([]candidateRequest, 0)
	for _, dependencySet := range dependencySets {
		requester := current.id.StringFull()
		for _, dependency := range dependencySet.Value {
			if !dependency.Mandatory && !options.WithOptional {
				continue
			}

			batchInputs = append(
				batchInputs, resolve.ConstraintInput{
					Requester:  requester,
					Dependency: dependency,
				},
			)

			childKey := dependency.Id.StringBase()
			if _, exists := planner.candidateGraph[childKey]; exists {
				continue
			}

			children = append(
				children, candidateRequest{
					id: dependency.Id,
					provenancePath: appendPath(
						current.provenancePath,
						requester,
					),
					mandatory: dependency.Mandatory,
				},
			)
		}
	}

	if len(batchInputs) > 0 {
		planner.constraintInputs = append(
			planner.constraintInputs,
			batchInputs...,
		)
		if _, err := resolve.MergeConstraintGraph(planner.constraintInputs); err != nil {
			return err
		}
	}

	planner.queue = append(planner.queue, children...)
	return nil
}

func (planner *candidateGraphPlanner) resolvedClosure() ResolvedClosure {
	if planner == nil {
		return ResolvedClosure{}
	}
	return ResolvedClosure{
		Roots:                append([]types.VersionedPackageRef(nil), planner.roots...),
		CandidateGraph:       cloneCandidateGraph(planner.candidateGraph),
		InstalledConstraints: append([]InstalledConstraint(nil), planner.installedConstraints...),
		Providers:            append([]upstream.PackageSource(nil), planner.providers...),
	}
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
		reasons = append(reasons, fmt.Sprintf("  - %s", providerErr.Error()))
	}
	return "provider failures:\n" + strings.Join(reasons, "\n")
}
