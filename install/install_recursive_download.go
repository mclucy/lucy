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
	tuiprogress "github.com/mclucy/lucy/tui/progress"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/util"
)

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

func pruneRecursiveCandidates(tx *RecursiveTransaction, excluded map[string]struct{}) {
	if tx == nil || len(excluded) == 0 {
		return
	}

	for key := range excluded {
		delete(tx.CandidateGraph, key)
	}
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

func downloadBatchPackages(
	workPath string,
	packages []types.Package,
) (stagingDir string, downloaded []types.Package, err error) {
	stagingDir, err = os.MkdirTemp("", "lucy_*")
	if err != nil {
		return "", nil, fmt.Errorf("create staging directory failed: %w", err)
	}

	if workPath != "." {
		if err := os.MkdirAll(workPath, 0o755); err != nil {
			return stagingDir, nil, fmt.Errorf("create server work path failed: %w", err)
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

			if ctx.Err() != nil {
				slots[index] = slot{failed: true, err: ctx.Err()}
				return
			}

			result, err := util.CachedDownload(
				pkg.Remote.FileUrl,
				stagingDir,
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
				cancel()
				slots[index] = slot{failed: true, err: err}
				return
			}

			if result.File != nil {
				pkg.Local = &types.PackageInstallation{Path: result.File.Name()}
				if err := result.File.Close(); err != nil {
					cancel()
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

	downloaded = make([]types.Package, 0, len(packages))
	failures := make([]string, 0)
	for i, item := range slots {
		if item.ok {
			downloaded = append(downloaded, item.pkg)
		}
		if item.failed {
			failures = append(failures, fmt.Sprintf("%s: %v", packages[i].Id.StringFull(), item.err))
		}
	}

	if len(failures) > 0 {
		return stagingDir, nil, fmt.Errorf(
			"failed to download packages: %s",
			strings.Join(failures, "; "),
		)
	}

	return stagingDir, downloaded, nil
}
