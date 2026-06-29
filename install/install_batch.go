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

	ids := requestsToIds(requests)
	prepared := prepareBatchIDs(ids)
	identityIds, regularIds := partitionBatchIDs(prepared)

	if err := validateIdentityCompatibility(identityIds); err != nil {
		return nil, installError(CategoryResolution, err, nil)
	}
	identityIds = sortIdentityPackages(identityIds)

	if len(identityIds) > 0 {
		recordEvent(
			journal,
			Event{
				Kind: EventBatchPhase, Header: "Installing platforms",
				IDs: identityIds,
			},
		)
		succeeded := make([]string, 0, len(identityIds))
		for _, id := range identityIds {
			if err := installPlatform(ctx, id, options); err != nil {
				if len(succeeded) > 0 {
					return nil, fmt.Errorf(
						"%w: failed to install %s (already installed: %s)",
						err,
						id.StringFull(),
						strings.Join(succeeded, ", "),
					)
				}
				return nil, fmt.Errorf(
					"failed to install %s: %w",
					id.StringFull(),
					err,
				)
			}
			succeeded = append(succeeded, id.StringFull())
		}
		workspace.InvalidateServerInfo()
	}

	if len(regularIds) == 0 {
		recordEvent(
			journal,
			Event{Kind: EventBatchSummary, Count: len(identityIds)},
		)
		return &Result{}, nil
	}

	plan, err := Plan(ctx, requests, options)
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

	ids := requestsToIds(requests)
	prepared := prepareBatchIDs(ids)
	_, regularIds := partitionBatchIDs(prepared)
	if len(regularIds) == 0 {
		return &ApplyPlan{}, nil
	}

	serverInfo := options.ServerInfo()
	recordEvent(
		journal,
		Event{
			Kind: EventBatchPhase, Header: "Fetching metadata for",
			IDs: regularIds,
		},
	)
	if err := validateRegularBatchIDs(regularIds, serverInfo); err != nil {
		return nil, installError(CategoryResolution, err, nil)
	}

	if serverInfo.Runtime == nil || serverInfo.Topology == nil || !serverInfo.Topology.Resolved() {
		return nil, installError(
			CategoryResolution,
			fmt.Errorf("runtime topology is unavailable"),
			nil,
		)
	}
	providers := options.Providers(*serverInfo.Topology)
	if providers == nil {
		providers = []upstream.PackageSource{}
	}

	if serverInfo.Environments.Mcdr != nil {
		mcdrProviders, err := routing.ResolveProviders(
			types.PlatformMCDR,
			types.SourceAuto,
		)
		if err != nil {
			log.Warn(fmt.Errorf("failed to resolve MCDR provider: %w", err))
		} else {
			providers = append(providers, mcdrProviders...)
		}
	}

	roots := append([]types.VersionedPackageRef(nil), regularIds...)
	serverLoader := serverInfo.DerivedModLoader()
	if serverLoader != types.PlatformAny {
		for i, id := range roots {
			if id.Platform == types.PlatformAny {
				roots[i].Platform = serverLoader
			}
		}
	}
	rootProviders, err := rootScopedProviders(
		serverInfo.Topology,
		requests,
		roots,
		serverLoader,
		providers,
	)
	if err != nil {
		return nil, installError(CategoryResolution, err, nil)
	}
	installedConstraints := snapshotInstalledConstraints(serverInfo)
	ambient, err := buildAmbientDependencies(ctx, serverInfo)
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
				providers:       providers,
				rootProviders:   rootProviders,
				rootProviderSet: keyedRoots(resolvePlan.Roots),
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

	serverInfo := options.ServerInfo()
	concretePlan := plan
	var err error
	if len(plan.Resolved.CandidateGraph) > 0 {
		downloaded, err := downloadArtifacts(
			ctx,
			plan.Resolved,
			serverInfo.Root,
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
		verified, err := verifyArtifacts(ctx, downloaded, options.Journal)
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
		serverInfo,
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
	var conflictErr *resolve.ConstraintConflictError
	if errors.As(err, &conflictErr) {
		return installError(CategoryConflict, err, nil)
	}
	return installError(CategoryResolution, err, nil)
}

func requestsToIds(requests []types.PackageRequest) []types.VersionedPackageRef {
	ids := make([]types.VersionedPackageRef, len(requests))
	for i, req := range requests {
		ids[i] = types.VersionedPackageRef{
			PackageRef: types.PackageRef{
				Platform: req.Platform,
				Name:     req.Name,
			},
			Version: req.Version,
		}
	}
	return ids
}

func rootScopedProviders(
	topology *types.RuntimeTopology,
	requests []types.PackageRequest,
	roots []types.VersionedPackageRef,
	serverLoader types.PlatformId,
	providers []upstream.PackageSource,
) (map[string][]upstream.PackageSource, error) {
	rootKeys := keyedRoots(roots)
	rootProviders := make(map[string][]upstream.PackageSource, len(rootKeys))
	rootScopes := make(map[string]types.SourceId, len(rootKeys))
	for _, req := range requests {
		id := types.VersionedPackageRef{
			PackageRef: req.PackageRef,
			Version:    req.Version,
		}
		if id.Version == types.VersionAny {
			id.Version = types.VersionCompatible
		}
		if id.Platform == types.PlatformAny && serverLoader != types.PlatformAny {
			id.Platform = serverLoader
		}
		for rootKey := range rootKeys {
			if rootKey != id.StringBase() {
				continue
			}
			if existing, ok := rootScopes[rootKey]; ok && existing != req.Scope {
				return nil, fmt.Errorf(
					"install: conflicting sources for %s: %s and %s",
					rootKey,
					existing,
					req.Scope,
				)
			}
			rootScopes[rootKey] = req.Scope
			if req.Scope == types.SourceAuto {
				rootProviders[rootKey] = providers
				break
			}
			scoped, err := routing.ResolveProvidersFromTopology(
				topology,
				req.Scope,
			)
			if err != nil {
				return nil, err
			}
			rootProviders[rootKey] = scoped
			break
		}
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

func prepareBatchIDs(ids []types.VersionedPackageRef) []types.VersionedPackageRef {
	seen := make(map[string]struct{}, len(ids))
	prepared := make([]types.VersionedPackageRef, 0, len(ids))

	for _, id := range ids {
		if id.Version == types.VersionAny {
			id.Version = types.VersionCompatible
		}

		key := id.StringBase()
		if _, ok := seen[key]; ok {
			continue
		}

		seen[key] = struct{}{}
		prepared = append(prepared, id)
	}

	return prepared
}

func partitionBatchIDs(ids []types.VersionedPackageRef) (
	[]types.VersionedPackageRef,
	[]types.VersionedPackageRef,
) {
	identityIds := make([]types.VersionedPackageRef, 0, len(ids))
	regularIds := make([]types.VersionedPackageRef, 0, len(ids))

	for _, id := range ids {
		if types.IsIdentityPackage(id.PackageRef) {
			identityIds = append(identityIds, id)
			continue
		}
		regularIds = append(regularIds, id)
	}

	return identityIds, regularIds
}

func validateRegularBatchIDs(
	ids []types.VersionedPackageRef,
	serverInfo workspace.Workspace,
) error {
	failures := make([]string, 0)

	for _, id := range ids {
		if err := ensureServerPlatformMatch(id, serverInfo); err != nil {
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
