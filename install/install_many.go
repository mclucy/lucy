package install

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
)

func InstallMany(ids []types.PackageId, source types.Source, options Options) (*Result, error) {
	const maxReconcileIterations = 3

	if len(ids) == 0 {
		return &Result{}, nil
	}

	prepared := prepareBatchIDs(ids)
	identityIds, regularIds := partitionBatchIDs(prepared)

	if err := validateIdentityCompatibility(identityIds); err != nil {
		return nil, err
	}
	identityIds = sortIdentityPackages(identityIds)

	if len(identityIds) > 0 {
		showBatchPhase("Installing platforms", identityIds)
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
				return nil, fmt.Errorf("failed to install %s: %w", id.StringFull(), err)
			}
			succeeded = append(succeeded, id.StringFull())
		}
		probe.InvalidateServerInfo()
	}

	if len(regularIds) == 0 {
		showBatchSummary(len(identityIds), 0)
		return &Result{}, nil
	}

	showBatchPhase("Fetching metadata for", regularIds)
	if err := validateRegularBatchIDs(regularIds); err != nil {
		return nil, err
	}

	serverInfo := probe.ServerInfo()
	providers, err := routing.ResolveProvidersFromTopology(
		serverInfo.Runtime.Topology,
		source,
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

	roots := append([]types.PackageId(nil), regularIds...)
	if serverLoader := serverInfo.Runtime.DerivedModLoader(); serverLoader != types.PlatformAny {
		for i, id := range roots {
			if id.Platform == types.PlatformAny {
				roots[i].Platform = serverLoader
			}
		}
	}
	seedTx := NewRecursiveTransaction(roots, providers)
	SnapshotInstalledConstraints(seedTx)
	resolvePlan := newRecursiveResolutionPlan(roots, seedTx.InstalledConstraints)
	var tx *RecursiveTransaction
	var diff ReconcileDiff

	for iteration := range maxReconcileIterations {
		showRecursiveResolveStart(resolvePlan.Roots)
		tx, err = BuildCandidateGraph(
			resolvePlan.Roots,
			providers,
			resolvePlan.InstalledConstraints,
			options,
		)
		if err != nil {
			showRecursiveConflict(err)
			return nil, err
		}
		pruneRecursiveCandidates(tx, resolvePlan.ExcludedCandidates)

		packages := recursiveCandidatePackages(tx)
		showRecursiveDownloadStart(len(packages))
		tx.StagingDir, packages, err = downloadBatchPackages(serverInfo.WorkPath, packages)
		if err != nil {
			return nil, err
		}
		backfillRecursiveDownloads(tx, packages)
		tx.AdvanceTo(PhaseDownloaded)

		showRecursiveVerifyStart(len(tx.DownloadedArtifacts))
		if err := VerifyDownloadedArtifacts(tx); err != nil {
			return nil, err
		}

		diff, err = ReconcileTransaction(tx)
		if err != nil {
			showRecursiveConflict(err)
			return nil, err
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

	plan, err := BuildRecursiveApplyPlan(tx)
	if err != nil {
		return nil, err
	}
	tx.SetApplyPlan(plan)
	tx.AdvanceTo(PhaseCommitted)

	if err := ApplyValidatedClosure(tx, serverInfo); err != nil {
		return nil, err
	}

	return buildInstallResult(tx), nil
}

func buildInstallResult(tx *RecursiveTransaction) *Result {
	if tx == nil || tx.Apply == nil {
		return &Result{}
	}

	installed := append([]types.Package(nil), tx.Apply.Install...)
	provenance := make(map[string][]string, len(tx.CandidateGraph))
	for key, node := range tx.CandidateGraph {
		provenance[key] = append([]string(nil), node.ProvenancePath...)
	}

	return &Result{Installed: installed, Provenance: provenance}
}

func prepareBatchIDs(ids []types.PackageId) []types.PackageId {
	seen := make(map[string]struct{}, len(ids))
	prepared := make([]types.PackageId, 0, len(ids))

	for _, id := range ids {
		if id.Version == types.VersionAny {
			id.Version = types.VersionCompatible
		}

		if id.IsIdentityPackage() {
			id.NormalizeIdentityPackage()
		}

		key := id.StringPlatformName()
		if _, ok := seen[key]; ok {
			continue
		}

		seen[key] = struct{}{}
		prepared = append(prepared, id)
	}

	return prepared
}

func partitionBatchIDs(ids []types.PackageId) ([]types.PackageId, []types.PackageId) {
	identityIds := make([]types.PackageId, 0, len(ids))
	regularIds := make([]types.PackageId, 0, len(ids))

	for _, id := range ids {
		if id.IsIdentityPackage() {
			identityIds = append(identityIds, id)
			continue
		}
		regularIds = append(regularIds, id)
	}

	return identityIds, regularIds
}

func validateRegularBatchIDs(ids []types.PackageId) error {
	failures := make([]string, 0)

	for _, id := range ids {
		if err := ensureServerPlatformMatch(id); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", id.StringFull(), err))
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
