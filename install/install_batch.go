package install

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/workspace"
)

const maxReconcileIterations = 3

func InstallMany(
	ctx context.Context,
	requests []types.PackageRequest,
	options InstallOptions,
) (
	*Result,
	error,
) {
	options = options.withDefaults()
	journal := options.Journal
	if err := ctx.Err(); err != nil {
		return nil, installError(CategoryResolution, err, nil)
	}
	if len(requests) == 0 {
		return &Result{}, nil
	}

	cores, regular := classifyInstallRequests(requests)
	if err := prepareCoreRequests(cores); err != nil {
		return nil, installError(CategoryResolution, err, nil)
	}

	currentEcosystem := types.EcoUnspecified
	if len(regular) > 0 {
		currentEcosystem = defaultRegularEcosystem(options.Workspace())
	}
	if _, _, err := prepareRegularRoots(
		regular,
		batchDefaultEcosystem(cores, currentEcosystem),
	); err != nil {
		return nil, installError(CategoryResolution, err, nil)
	}

	coreIDs := coreRequestIDs(cores)
	if len(cores) > 0 {
		recordEvent(
			journal,
			Event{
				Kind:   EventBatchPhase,
				Header: "Installing server components",
				IDs:    coreIDs,
			},
		)
		succeeded := make([]string, 0, len(cores))
		for i, request := range cores {
			if err := installCorePackage(ctx, request, options); err != nil {
				if len(succeeded) > 0 {
					return nil, fmt.Errorf(
						"%w: failed to install %s (already installed: %s)",
						err,
						coreIDs[i].StringFull(),
						strings.Join(succeeded, ", "),
					)
				}
				return nil, fmt.Errorf(
					"failed to install %s: %w",
					coreIDs[i].StringFull(),
					err,
				)
			}
			succeeded = append(succeeded, coreIDs[i].StringFull())
		}
		workspace.Invalidate()
	}

	if len(regular) == 0 {
		recordEvent(
			journal,
			Event{Kind: EventBatchSummary, Count: len(cores)},
		)
		return &Result{}, nil
	}

	plan, err := Plan(ctx, regular, options)
	if err != nil {
		return nil, err
	}
	return Apply(ctx, *plan, options)
}

func Plan(
	ctx context.Context,
	requests []types.PackageRequest,
	options InstallOptions,
) (*ApplyPlan, error) {
	options = options.withDefaults()
	journal := options.Journal
	if err := ctx.Err(); err != nil {
		return nil, installError(CategoryResolution, err, nil)
	}

	if len(requests) == 0 {
		return &ApplyPlan{}, nil
	}

	cores, regular := classifyInstallRequests(requests)
	if len(cores) > 0 {
		return nil, installError(
			CategoryResolution,
			fmt.Errorf(
				"core packages require Install or InstallMany: %s",
				cores[0].Match.Core,
			),
			nil,
		)
	}
	if len(regular) == 0 {
		return &ApplyPlan{}, nil
	}

	ws := options.Workspace()
	defaultEcosystem := defaultRegularEcosystem(ws)
	effectiveRequests, roots, err := prepareRegularRoots(
		regular,
		defaultEcosystem,
	)
	if err != nil {
		return nil, installError(CategoryResolution, err, nil)
	}
	recordEvent(
		journal,
		Event{
			Kind: EventBatchPhase, Header: "Fetching metadata for",
			IDs: roots,
		},
	)
	if err := validateRegularBatchIDs(roots, ws); err != nil {
		return nil, installError(CategoryResolution, err, nil)
	}

	if ws.Server() == nil {
		return nil, installError(
			CategoryResolution,
			fmt.Errorf("server runtime is unavailable"),
			nil,
		)
	}
	providers := options.Providers(ws)
	if providers == nil {
		providers = []upstream.PackageSource{}
	}

	if ws.Environments.Mcdr != nil {
		mcdrProviders, err := routing.ResolveProviders(
			types.EcoMcdr,
			types.SourceAuto,
		)
		if err != nil {
			log.Warn(fmt.Errorf("failed to resolve MCDR provider: %w", err))
		} else {
			providers = append(providers, mcdrProviders...)
		}
	}

	rootProviders, err := rootScopedProviders(
		ws,
		effectiveRequests,
		roots,
		providers,
	)
	if err != nil {
		return nil, installError(CategoryResolution, err, nil)
	}
	installedConstraints := snapshotInstalledConstraints(ws)
	ambient, err := buildAmbientDependencies(ctx, ws)
	if err != nil {
		return nil, installError(CategoryResolution, err, nil)
	}
	resolvePlan := newRecursiveResolutionPlan(
		roots,
		installedConstraints,
	)
	var resolved ResolvedClosure

	for iteration := range maxReconcileIterations {
		recordEvent(
			journal,
			Event{Kind: EventResolveStart, Roots: resolvePlan.Roots},
		)
		resolved, err = resolveClosure(
			ctx,
			resolvePlan.Roots,
			providers,
			resolvePlan.InstalledConstraints,
			ambient,
			options,
			providerCandidateResolver{
				providers:        providers,
				rootProviders:    rootProviders,
				rootProviderSet:  keyedRoots(resolvePlan.Roots),
				defaultEcosystem: defaultEcosystem,
			},
		)
		if err != nil {
			recordEvent(journal, Event{Kind: EventConflict, Err: err})
			return nil, resolutionError(err)
		}
		pruneRecursiveCandidates(&resolved, resolvePlan.ExcludedCandidates)

		diff, err := computeReconcileDiff(
			resolved,
			resolvePlan.InstalledConstraints,
			journal,
		)
		if err != nil {
			recordEvent(journal, Event{Kind: EventConflict, Err: err})
			return nil, installError(CategoryResolution, err, nil)
		}
		if diff.IsStable() {
			break
		}

		if iteration == maxReconcileIterations-1 {
			return nil, fmt.Errorf(
				"install: recursive closure did not stabilize after %d iterations: %s",
				maxReconcileIterations,
				summarizeReconcileDiff(diff),
			)
		}

		resolvePlan = refineRecursiveResolutionPlan(resolvePlan, diff)
	}

	return &ApplyPlan{
		Resolved: resolved,
		InstalledConstraints: append(
			[]InstalledConstraint(nil),
			installedConstraints...,
		),
	}, nil
}

func Apply(
	ctx context.Context,
	plan ApplyPlan,
	options InstallOptions,
) (*Result, error) {
	options = options.withDefaults()
	if err := ctx.Err(); err != nil {
		return nil, installError(CategoryApply, err, nil)
	}

	ws := options.Workspace()
	concretePlan := plan
	var err error
	if len(plan.Resolved.CandidateGraph) > 0 {
		downloaded, err := downloadArtifacts(
			ctx,
			plan.Resolved,
			ws.Root,
			options,
			options.Journal,
		)
		if err != nil {
			return nil, installError(CategoryDownload, err, nil)
		}

		recordEvent(
			options.Journal,
			Event{
				Kind:  EventVerifyStart,
				Count: len(downloaded.DownloadedArtifacts),
			},
		)
		verified, err := verifyArtifacts(
			ctx,
			downloaded,
			options.Journal,
			ws.Server(),
		)
		if err != nil {
			return nil, installError(CategoryVerify, err, nil)
		}

		concretePlan, err = planApply(ctx, verified, plan.InstalledConstraints)
		if err != nil {
			return nil, installError(CategoryApply, err, nil)
		}
	}

	concretePlan, err = applyPlan(
		ctx,
		concretePlan,
		ws,
		options.Journal,
	)
	if err != nil {
		return nil, installError(CategoryApply, err, nil)
	}

	return buildInstallResultFromPlan(concretePlan), nil
}

func buildInstallResultFromPlan(plan ApplyPlan) *Result {
	installed := append([]types.InstalledPackage(nil), plan.Install...)
	provenance := make(map[string][]string, len(plan.Provenance))
	for key, path := range plan.Provenance {
		provenance[key] = append([]string(nil), path...)
	}

	return &Result{Installed: installed, Provenance: provenance}
}

func resolutionError(err error) error {
	if _, ok := errors.AsType[*resolve.ConstraintConflictError](err); ok {
		return installError(CategoryConflict, err, nil)
	}
	return installError(CategoryResolution, err, nil)
}

func rootScopedProviders(
	ws workspace.Workspace,
	requests []types.PackageRequest,
	roots []types.VersionedPackageRef,
	providers []upstream.PackageSource,
) (map[string][]upstream.PackageSource, error) {
	rootKeys := keyedRoots(roots)
	rootProviders := make(map[string][]upstream.PackageSource, len(rootKeys))

	for _, request := range requests {
		rootKey := request.PackageRef.StringBase()
		if _, ok := rootKeys[rootKey]; !ok {
			continue
		}
		if request.Scope == types.SourceAuto {
			rootProviders[rootKey] = providers
			continue
		}
		scoped, err := routing.ResolveProvidersForRuntime(
			effectiveRuntimeEcosystems(ws),
			request.Scope,
		)
		if err != nil {
			return nil, err
		}
		rootProviders[rootKey] = scoped
	}

	return rootProviders, nil
}

func keyedRoots(roots []types.VersionedPackageRef) map[string]struct{} {
	keys := make(map[string]struct{}, len(roots))
	for _, root := range roots {
		keys[root.StringBase()] = struct{}{}
	}
	return keys
}

func validateRegularBatchIDs(
	ids []types.VersionedPackageRef,
	ws workspace.Workspace,
) error {
	failures := make([]string, 0)

	for _, id := range ids {
		if err := ensureServerEcosystemMatch(id, ws); err != nil {
			failures = append(
				failures,
				fmt.Sprintf("%s: %v", id.StringFull(), err),
			)
		}
	}

	if len(failures) == 0 {
		return nil
	}

	return fmt.Errorf(
		"server compatibility check failed: %s",
		strings.Join(failures, "; "),
	)
}
