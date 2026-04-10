package install

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/probe"
	tuiprogress "github.com/mclucy/lucy/tui/progress"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/util"
)

func InstallMany(ids []types.PackageId, source types.Source) error {
	if len(ids) == 0 {
		return nil
	}

	if len(ids) == 1 {
		return Install(ids[0], source)
	}

	prepared := prepareBatchIDs(ids)
	identityIds, regularIds := partitionBatchIDs(prepared)

	if err := validateIdentityCompatibility(identityIds); err != nil {
		return err
	}
	identityIds = sortIdentityPackages(identityIds)

	if len(identityIds) > 0 {
		showBatchPhase("Installing platforms", identityIds)
		for _, id := range identityIds {
			if err := installPlatform(id); err != nil {
				return err
			}
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

	packages, err := resolveBatchPackages(regularIds, providers)
	if err != nil {
		return err
	}

	packages, err = downloadBatchPackages(serverInfo.WorkPath, packages)
	if err != nil {
		return err
	}

	installed := len(identityIds)
	failed := 0
	installErrors := make([]string, 0)
	for _, p := range packages {
		logger.ShowInfo(fmt.Sprintf("==> Installing %s", p.Id.StringFull()))

		installer := installers[p.Id.Platform]
		if installer == nil {
			installer = installers[types.PlatformAny]
		}
		if installer == nil {
			failed++
			installErrors = append(
				installErrors,
				fmt.Sprintf("%s: no installer found", p.Id.StringFull()),
			)
			continue
		}

		if err := installer(p); err != nil {
			failed++
			installErrors = append(
				installErrors,
				fmt.Sprintf("%s: %v", p.Id.StringFull(), err),
			)
			continue
		}

		installed++
	}

	showBatchSummary(installed, failed)
	if len(installErrors) > 0 {
		return fmt.Errorf(
			"failed to install packages: %s",
			strings.Join(installErrors, "; "),
		)
	}

	return nil
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

func resolveBatchPackages(
	ids []types.PackageId,
	providers []upstream.Provider,
) ([]types.Package, error) {
	packages := make([]types.Package, 0, len(ids))
	failures := make([]string, 0)

	for _, id := range ids {
		fetches, _ := routing.FetchMany(providers, id)
		if len(fetches) == 0 {
			failures = append(failures, id.StringFull())
			continue
		}

		fetch := fetches[0]
		packages = append(packages, types.Package{
			Id:     fetch.ResolvedID,
			Remote: &fetch.Remote,
		})
	}

	if len(failures) > 0 {
		return nil, fmt.Errorf(
			"no candidates found for %s",
			strings.Join(failures, ", "),
		)
	}

	return packages, nil
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

	for i, p := range packages {
		tracker := tuiprogress.NewTracker(p.Id.StringFull())

		wg.Add(1)
		go func(index int, pkg types.Package, tracker *tuiprogress.Tracker) {
			defer wg.Done()
			defer tracker.Close()

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
				slots[index] = slot{failed: true, err: err}
				return
			}

			if result.File != nil {
				pkg.Local = &types.PackageInstallation{Path: result.File.Name()}
				if err := result.File.Close(); err != nil {
					slots[index] = slot{failed: true, err: err}
					return
				}
			}

			slots[index] = slot{ok: true, pkg: pkg}
		}(i, p, tracker)
	}

	wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := tuiprogress.WaitForShutdown(ctx); err != nil {
		return nil, fmt.Errorf("progress shutdown failed: %w", err)
	}

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
