package install

import (
	"context"
	"fmt"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/probe"
	tuiprogress "github.com/mclucy/lucy/tui/progress"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/util"
)

func InstallMany(ids []types.PackageId, source types.Source) error {
	const maxReconcileIterations = 3

	if len(ids) == 0 {
		return nil
	}

	prepared := prepareBatchIDs(ids)
	identityIds, regularIds := partitionBatchIDs(prepared)

	if err := validateIdentityCompatibility(identityIds); err != nil {
		return err
	}
	identityIds = sortIdentityPackages(identityIds)

	if len(identityIds) > 0 {
		showBatchPhase("Installing platforms", identityIds)
		succeeded := make([]string, 0, len(identityIds))
		for _, id := range identityIds {
			if err := installPlatform(id); err != nil {
				if len(succeeded) > 0 {
					return fmt.Errorf(
						"%s: failed to install %s (already installed: %s)",
						err,
						id.StringFull(),
						strings.Join(succeeded, ", "),
					)
				}
				return fmt.Errorf("failed to install %s: %w", id.StringFull(), err)
			}
			succeeded = append(succeeded, id.StringFull())
		}
		probe.InvalidateServerInfo()
	}

	if len(regularIds) == 0 {
		showBatchSummary(len(identityIds), 0)
		return nil
	}

	showBatchPhase("Fetching metadata for", regularIds)
	if err := validateRegularBatchIDs(regularIds); err != nil {
		return err
	}

	serverInfo := probe.ServerInfo()
	providers, err := routing.ResolveProvidersFromTopology(
		serverInfo.Runtime.Topology,
		source,
	)
	if err != nil {
		return err
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
	excluded := map[string]struct{}{}
	seedTx := NewRecursiveTransaction(roots, providers)
	SnapshotInstalledConstraints(seedTx)
	baseConstraints := append([]InstalledConstraint(nil), seedTx.InstalledConstraints...)
	feedbackConstraints := []InstalledConstraint{}
	var tx *RecursiveTransaction
	var diff ReconcileDiff

	for iteration := range maxReconcileIterations {
		showRecursiveResolveStart(roots)
		tx, err = BuildCandidateGraph(
			roots,
			providers,
			mergeReconcileConstraints(baseConstraints, feedbackConstraints),
		)
		if err != nil {
			showRecursiveConflict(err)
			return err
		}
		pruneRecursiveCandidates(tx, excluded)

		packages := recursiveCandidatePackages(tx)
		showRecursiveDownloadStart(len(packages))
		packages, err = downloadBatchPackages(serverInfo.WorkPath, packages)
		if err != nil {
			return err
		}
		backfillRecursiveDownloads(tx, packages)
		tx.AdvanceTo(PhaseDownloaded)

		showRecursiveVerifyStart(len(tx.DownloadedArtifacts))
		if err := VerifyDownloadedArtifacts(tx); err != nil {
			return err
		}

		diff, err = ReconcileTransaction(tx)
		if err != nil {
			showRecursiveConflict(err)
			return err
		}
		if diff.IsStable() {
			break
		}

		if iteration == maxReconcileIterations-1 {
			return fmt.Errorf(
				"install: recursive closure did not stabilize after %d iterations: %s",
				maxReconcileIterations,
				reconcileDiffSummary(diff),
			)
		}

		roots = appendMissingRoots(roots, diff.Missing)
		feedbackConstraints = mergeReconcileConstraints(
			feedbackConstraints,
			tightenedConstraintInputs(diff.Tightened),
		)
		excluded = make(map[string]struct{}, len(diff.Extra))
		for _, id := range diff.Extra {
			excluded[id.StringPlatformName()] = struct{}{}
		}
	}

	plan, err := buildRecursiveApplyPlan(tx)
	if err != nil {
		return err
	}
	tx.SetApplyPlan(plan)
	tx.AdvanceTo(PhaseCommitted)

	return ApplyValidatedClosure(tx, serverInfo)
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

func recursiveCandidatePackages(tx *RecursiveTransaction) []types.Package {
	if tx == nil {
		return nil
	}

	keys := make([]string, 0, len(tx.CandidateGraph))
	for key, node := range tx.CandidateGraph {
		if node.Package.Remote == nil {
			continue
		}
		keys = append(keys, key)
	}
	slices.Sort(keys)

	packages := make([]types.Package, 0, len(keys))
	for _, key := range keys {
		packages = append(packages, tx.CandidateGraph[key].Package)
	}

	return packages
}

func backfillRecursiveDownloads(tx *RecursiveTransaction, packages []types.Package) {
	if tx == nil {
		return
	}

	for _, pkg := range packages {
		if pkg.Local == nil {
			continue
		}

		tx.DownloadedArtifacts[pkg.Id.StringFull()] = pkg.Local.Path

		key := pkg.Id.StringPlatformName()
		node, ok := tx.CandidateGraph[key]
		if !ok {
			continue
		}
		node.Package.Local = pkg.Local
		tx.CandidateGraph[key] = node
	}
}

func buildRecursiveApplyPlan(tx *RecursiveTransaction) (ApplyPlan, error) {
	if tx == nil {
		return ApplyPlan{}, fmt.Errorf("install: nil recursive transaction")
	}

	// Build a name-only index of candidate nodes to handle platform mismatch
	// between advisory (e.g. none/create) and verified (e.g. neoforge/create).
	candidateByName := make(map[types.ProjectName]CandidateNode, len(tx.CandidateGraph))
	for _, node := range tx.CandidateGraph {
		if node.Package.Remote != nil {
			candidateByName[node.Package.Id.Name] = node
		}
	}

	keys := make([]string, 0, len(tx.VerifiedGraph))
	for key := range tx.VerifiedGraph {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	install := make([]types.Package, 0, len(keys))
	for _, key := range keys {
		verified := tx.VerifiedGraph[key].Package

		candidate, ok := tx.CandidateGraph[key]
		if !ok || candidate.Package.Remote == nil {
			candidate, ok = candidateByName[verified.Id.Name]
		}
		if !ok || candidate.Package.Remote == nil {
			return ApplyPlan{}, fmt.Errorf(
				"install: verified package %s is missing candidate remote metadata",
				verified.Id.StringFull(),
			)
		}

		pkg := verified
		pkg.Remote = candidate.Package.Remote
		install = append(install, pkg)
	}

	// Collect extra candidate nodes (advisory-only, not in the verified closure)
	// that have a downloaded local artifact — they must be removed from disk.
	remove := make([]types.Package, 0)
	for _, extraId := range tx.ReconcileDiff.Extra {
		key := extraId.StringPlatformName()
		node, ok := tx.CandidateGraph[key]
		if !ok {
			continue
		}
		if node.Package.Local == nil || node.Package.Local.Path == "" {
			continue
		}
		remove = append(remove, node.Package)
	}

	return ApplyPlan{Install: install, Remove: remove}, nil
}

func reconcileDiffSummary(diff ReconcileDiff) string {
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

func pruneRecursiveCandidates(tx *RecursiveTransaction, excluded map[string]struct{}) {
	if tx == nil || len(excluded) == 0 {
		return
	}

	for key := range excluded {
		delete(tx.CandidateGraph, key)
	}
}

func appendMissingRoots(existing []types.PackageId, missing []types.PackageId) []types.PackageId {
	if len(missing) == 0 {
		return existing
	}

	seen := make(map[string]struct{}, len(existing)+len(missing))
	updated := make([]types.PackageId, 0, len(existing)+len(missing))
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
		constraints = append(constraints, InstalledConstraint{ConstraintInput: input})
	}
	return constraints
}

func reconcileConstraintInputKey(input ConstraintInput) string {
	return input.Requester + "|" + input.Dependency.Id.StringPlatformName()
}

func downloadBatchPackages(
	workPath string,
	packages []types.Package,
) ([]types.Package, error) {
	if workPath != "." {
		if err := os.MkdirAll(workPath, 0o755); err != nil {
			return nil, fmt.Errorf("create server work path failed: %w", err)
		}
	}

	resolvedIds := make([]types.PackageId, len(packages))
	for i, p := range packages {
		resolvedIds[i] = p.Id
	}
	showBatchPhase("Downloading", resolvedIds)

	type slot struct {
		pkg    types.Package
		err    error
		ok     bool
		failed bool
	}

	slots := make([]slot, len(packages))
	var wg sync.WaitGroup

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for i, p := range packages {
		tracker := tuiprogress.NewTracker(p.Id.StringFull())

		wg.Add(1)
		go func(index int, pkg types.Package, tracker *tuiprogress.Tracker) {
			defer wg.Done()
			defer tracker.Close()

			// Check if already cancelled by a sibling failure
			if ctx.Err() != nil {
				slots[index] = slot{failed: true, err: ctx.Err()}
				return
			}

			result, err := util.CachedDownload(
				pkg.Remote.FileUrl,
				workPath,
				util.DownloadOptions{
					Kind:          cache.KindArtifact,
					Filename:      pkg.Remote.Filename,
					ExpectedHash:  pkg.Remote.Hash,
					HashAlgorithm: cache.ParseHashAlgorithm(pkg.Remote.HashAlgorithm),
					WrapReader:    tracker.ProxyReader,
					OnResolvedFilename: func(name string) {
						tracker.SetTitle(name)
					},
					OnCacheHit: tracker.CacheHit,
				},
			)
			if err != nil {
				cancel() // signal other goroutines to abort
				slots[index] = slot{failed: true, err: err}
				return
			}

			if result.File != nil {
				pkg.Local = &types.PackageInstallation{Path: result.File.Name()}
				if err := result.File.Close(); err != nil {
					cancel() // signal other goroutines to abort
					slots[index] = slot{failed: true, err: err}
					return
				}
			}

			slots[index] = slot{ok: true, pkg: pkg}
		}(i, p, tracker)
	}

	wg.Wait()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	_ = tuiprogress.WaitForShutdown(shutdownCtx)

	downloaded := make([]types.Package, 0, len(packages))
	failures := make([]string, 0)
	for i, item := range slots {
		if item.ok {
			downloaded = append(downloaded, item.pkg)
		}
		if item.failed {
			failures = append(
				failures,
				fmt.Sprintf("%s: %v", packages[i].Id.StringFull(), item.err),
			)
		}
	}

	if len(failures) > 0 {
		return nil, fmt.Errorf(
			"failed to download packages: %s",
			strings.Join(failures, "; "),
		)
	}

	return downloaded, nil
}
