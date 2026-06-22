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
)

func downloadArtifacts(
	ctx context.Context,
	resolved ResolvedClosure,
	serverRoot string,
	options InstallOptions,
	journal Journal,
) (DownloadedClosure, error) {
	if err := ctx.Err(); err != nil {
		return DownloadedClosure{}, err
	}

	packages := recursiveCandidatePackages(resolved.CandidateGraph)
	recordEvent(journal, Event{Kind: EventDownloadStart, Count: len(packages)})

	stagingDir, downloadedPackages, err := downloadBatchPackages(
		ctx,
		serverRoot,
		packages,
		options,
		journal,
	)
	if err != nil {
		return DownloadedClosure{}, err
	}

	resolved.CandidateGraph = cloneCandidateGraph(resolved.CandidateGraph)
	resolved.StagingDir = stagingDir
	downloadedArtifacts := backfillRecursiveDownloads(
		resolved.CandidateGraph,
		downloadedPackages,
	)

	return DownloadedClosure{
		Resolved:            resolved,
		DownloadedArtifacts: downloadedArtifacts,
	}, nil
}

func recursiveCandidatePackages(candidateGraph map[string]CandidateNode) []types.ResolvedPackage {
	keys := make([]string, 0, len(candidateGraph))
	for key, node := range candidateGraph {
		if node.Package.FileUrl == "" {
			continue
		}
		keys = append(keys, key)
	}
	slices.Sort(keys)

	packages := make([]types.ResolvedPackage, 0, len(keys))
	for _, key := range keys {
		packages = append(packages, candidateGraph[key].Package)
	}

	return packages
}

func pruneRecursiveCandidates(
	resolved *ResolvedClosure,
	excluded map[string]struct{},
) {
	if resolved == nil || len(excluded) == 0 {
		return
	}

	for key := range excluded {
		delete(resolved.CandidateGraph, key)
	}
}

func backfillRecursiveDownloads(
	candidateGraph map[string]CandidateNode,
	packages []downloadedPackage,
) map[string]string {
	downloadedArtifacts := make(map[string]string, len(packages))
	for _, pkg := range packages {
		if pkg.Path == "" {
			continue
		}

		downloadedArtifacts[resolvedPackageLabel(pkg.Package)] = pkg.Path

		key := pkg.Package.Id.StringBase()
		node, ok := candidateGraph[key]
		if !ok {
			continue
		}
		node.Path = pkg.Path
		candidateGraph[key] = node
	}

	return downloadedArtifacts
}

func cloneCandidateGraph(source map[string]CandidateNode) map[string]CandidateNode {
	clone := make(map[string]CandidateNode, len(source))
	for key, node := range source {
		node.ProvenancePath = append([]string(nil), node.ProvenancePath...)
		clone[key] = node
	}
	return clone
}

func downloadBatchPackages(
	ctx context.Context,
	workPath string,
	packages []types.ResolvedPackage,
	options InstallOptions,
	journal Journal,
) (stagingDir string, downloaded []downloadedPackage, err error) {
	options = options.withDefaults()
	if err := ctx.Err(); err != nil {
		return "", nil, err
	}

	stagingDir, err = os.MkdirTemp("", "lucy_*")
	if err != nil {
		return "", nil, fmt.Errorf("create staging directory failed: %w", err)
	}

	if workPath != "." {
		if err := os.MkdirAll(workPath, 0o755); err != nil {
			return stagingDir, nil, fmt.Errorf(
				"create server work path failed: %w",
				err,
			)
		}
	}

	resolvedIds := make([]types.VersionedPackageRef, len(packages))
	for i, p := range packages {
		resolvedIds[i] = versionedResolvedID(p)
	}
	recordEvent(journal, Event{Kind: EventBatchPhase, Header: "Downloading", IDs: resolvedIds})

	type slot struct {
		pkg    downloadedPackage
		err    error
		ok     bool
		failed bool
	}

	slots := make([]slot, len(packages))
	var wg sync.WaitGroup

	downloadCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	for i, p := range packages {
		tracker := tuiprogress.NewTracker(p.Id.StringFull())

		wg.Add(1)
		go func(index int, pkg types.ResolvedPackage, tracker *tuiprogress.Tracker) {
			defer wg.Done()
			defer tracker.Close()

			if downloadCtx.Err() != nil {
				slots[index] = slot{failed: true, err: downloadCtx.Err()}
				return
			}

			result, err := options.Cache(
				pkg.FileUrl,
				stagingDir,
				cache.DownloadOptions{
					Kind:          cache.KindArtifact,
					Filename:      pkg.Filename,
					ExpectedHash:  pkg.Hash,
					HashAlgorithm: cache.ParseHashAlgorithm(pkg.HashAlgorithm),
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

			downloadedPkg := downloadedPackage{Package: pkg}
			if result.File != nil {
				downloadedPkg.Path = result.File.Name()
				if err := result.File.Close(); err != nil {
					cancel()
					slots[index] = slot{failed: true, err: err}
					return
				}
			}

			slots[index] = slot{ok: true, pkg: downloadedPkg}
		}(i, p, tracker)
	}

	wg.Wait()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer shutdownCancel()
	_ = tuiprogress.WaitForShutdown(shutdownCtx)

	downloaded = make([]downloadedPackage, 0, len(packages))
	failures := make([]string, 0)
	for i, item := range slots {
		if item.ok {
			downloaded = append(downloaded, item.pkg)
		}
		if item.failed {
			failures = append(
				failures,
				fmt.Sprintf("%s: %v", resolvedPackageLabel(packages[i]), item.err),
			)
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
