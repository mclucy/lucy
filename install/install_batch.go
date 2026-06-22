package install

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/workspace"
)

func InstallMany(requests []types.PackageRequest, options InstallOptions) (
	*Result,
	error,
) {
	const maxReconcileIterations = 3
	options = options.withDefaults()
	journal := options.Journal

	if len(requests) == 0 {
		return &Result{}, nil
	}

	ids := requestsToIds(requests)
	prepared := prepareBatchIDs(ids)
	identityIds, regularIds := partitionBatchIDs(prepared)

	if err := validateIdentityCompatibility(identityIds); err != nil {
		return nil, err
	}
	identityIds = sortIdentityPackages(identityIds)

	if len(identityIds) > 0 {
		recordEvent(journal, Event{Kind: EventBatchPhase, Header: "Installing platforms", IDs: identityIds})
		succeeded := make([]string, 0, len(identityIds))
		for _, id := range identityIds {
			if err := installPlatform(id); err != nil {
				if len(succeeded) > 0 {
					return nil, fmt.Errorf(
						"%s: failed to install %s (already installed: %s)",
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
		recordEvent(journal, Event{Kind: EventBatchSummary, Count: len(identityIds)})
		return &Result{}, nil
	}

	recordEvent(journal, Event{Kind: EventBatchPhase, Header: "Fetching metadata for", IDs: regularIds})
	if err := validateRegularBatchIDs(regularIds); err != nil {
		return nil, err
	}

	serverInfo := workspace.ServerInfo()
	providers, err := routing.ResolveProvidersFromTopology(
		serverInfo.Runtime.Topology,
		types.SourceAuto,
	)
	if err != nil {
		return nil, err
	}

	if serverInfo.Environments.Mcdr != nil {
		mcdrProviders, err := routing.ResolveProviders(
			types.PlatformMCDR,
			types.SourceAuto,
		)
		if err != nil {
			logger.ShowInfo(
				fmt.Errorf("failed to resolve MCDR provider: %w", err),
			)
		} else {
			providers = append(providers, mcdrProviders...)
		}
	}

	roots := append([]types.VersionedPackageRef(nil), regularIds...)
	serverLoader := serverInfo.Runtime.DerivedModLoader()
	if serverLoader != types.PlatformAny {
		for i, id := range roots {
			if id.Platform == types.PlatformAny {
				roots[i].Platform = serverLoader
			}
		}
	}
	rootProviders, err := rootScopedProviders(
		serverInfo.Runtime.Topology,
		requests,
		roots,
		serverLoader,
		providers,
	)
	if err != nil {
		return nil, err
	}
	installedConstraints := SnapshotInstalledConstraints()
	resolvePlan := newRecursiveResolutionPlan(
		roots,
		installedConstraints,
	)
	var resolved ResolvedClosure
	var downloaded DownloadedClosure
	var verified VerifiedClosure

	for iteration := range maxReconcileIterations {
		recordEvent(journal, Event{Kind: EventResolveStart, Roots: resolvePlan.Roots})
		resolved, err = resolveClosure(
			resolvePlan.Roots,
			providers,
			resolvePlan.InstalledConstraints,
			options,
			providerCandidateResolver{
				providers:       providers,
				rootProviders:   rootProviders,
				rootProviderSet: keyedRoots(resolvePlan.Roots),
			},
		)
		if err != nil {
			recordEvent(journal, Event{Kind: EventConflict, Err: err})
			return nil, err
		}
		pruneRecursiveCandidates(&resolved, resolvePlan.ExcludedCandidates)

		downloaded, err = downloadArtifacts(resolved, serverInfo.Root, journal)
		if err != nil {
			return nil, err
		}

		recordEvent(journal, Event{Kind: EventVerifyStart, Count: len(downloaded.DownloadedArtifacts)})
		verified, err = verifyArtifacts(downloaded, journal)
		if err != nil {
			recordEvent(journal, Event{Kind: EventConflict, Err: err})
			return nil, err
		}
		if verified.ReconcileDiff.IsStable() {
			break
		}

		if iteration == maxReconcileIterations-1 {
			return nil, fmt.Errorf(
				"install: recursive closure did not stabilize after %d iterations: %s",
				maxReconcileIterations,
				summarizeReconcileDiff(verified.ReconcileDiff),
			)
		}

		resolvePlan = refineRecursiveResolutionPlan(resolvePlan, verified.ReconcileDiff)
	}

	plan, err := planApply(verified, installedConstraints)
	if err != nil {
		return nil, err
	}

	if err := applyPlan(plan, serverInfo, journal); err != nil {
		return nil, err
	}

	return buildInstallResultFromPlan(plan, resolved.CandidateGraph), nil
}

func buildInstallResultFromPlan(
	plan ApplyPlan,
	candidateGraph map[string]CandidateNode,
) *Result {
	installed := append([]types.Package(nil), plan.Install...)
	provenance := make(map[string][]string, len(candidateGraph))
	for key, node := range candidateGraph {
		provenance[key] = append([]string(nil), node.ProvenancePath...)
	}

	return &Result{Installed: installed, Provenance: provenance}
}

// TODO(package-ref-migration) — boundary conversion; pipeline internals still use PackageId
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

func validateRegularBatchIDs(ids []types.VersionedPackageRef) error {
	failures := make([]string, 0)

	for _, id := range ids {
		if err := ensureServerPlatformMatch(id); err != nil {
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
