package install

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
)

const installFileWorkers = 4

// stagePackages validates ownership and destinations before acquiring or
// copying independent package artifacts.
func (s *Service) stagePackages(ctx context.Context, root, stage string, resolved *document.Lock, store *downloadStore, stagedOutputs map[string]bootstrap.Output, opts Options) (map[string]bool, map[string]string, error) {
	if err := rejectSharedDestinations(root, resolved, stagedOutputs); err != nil {
		return nil, nil, err
	}
	type job struct {
		pkg         *document.Package
		destination string
	}
	jobs := make([]job, 0, len(resolved.Packages))
	overwritePaths := map[string]bool{}
	for i := range resolved.Packages {
		p := &resolved.Packages[i]
		if !p.Enabled() {
			continue
		}
		destination, err := resolved.PackageDestination(p)
		if err != nil {
			return nil, nil, err
		}
		if opts.Overwrite {
			overwritePaths[filepath.ToSlash(destination)] = true
		}
		jobs = append(jobs, job{pkg: p, destination: destination})
	}
	if len(jobs) == 0 {
		return overwritePaths, nil, nil
	}
	const phase = "package staging"
	started := time.Now()
	opts.Trace.phaseStarted(phase)
	completed := 0
	hashes := make([]string, len(jobs))
	err := runStagingJobs(ctx, len(jobs), store.injected != nil, func(i int) error {
		j := jobs[i]
		source, err := store.materializeFor(ctx, &j.pkg.Artifact, filepath.ToSlash(j.destination))
		if err != nil {
			return err
		}
		j.pkg.Artifact.Hashes = source.Hashes
		hashes[i] = strings.ToLower(source.Hashes["sha256"])
		if err := fsutil.Materialize(source.Path, filepath.Join(stage, j.destination), s.link.Perm(0o644), s.link); err != nil {
			return err
		}
		store.progressMu.Lock()
		completed++
		opts.Trace.ReportPhaseProgress(phase, completed, len(jobs))
		store.progressMu.Unlock()
		return nil
	})
	if err != nil {
		return nil, nil, err
	}
	opts.Trace.phaseDone(PhaseInfo{Name: phase, Elapsed: time.Since(started), Count: len(jobs), Total: len(jobs)})
	s.logger.Debug("packages staged", "active", len(jobs), "recorded", len(resolved.Packages))
	stagedHashes := make(map[string]string, len(jobs))
	for i, j := range jobs {
		if hashes[i] == "" {
			continue
		}
		key := filepath.ToSlash(filepath.Clean(filepath.FromSlash(j.destination)))
		stagedHashes[key] = hashes[i]
	}
	return overwritePaths, stagedHashes, nil
}

// runStagingJobs runs each job with at most installFileWorkers in flight, or
// one at a time for an injected source. A job starts as soon as a worker frees
// up, so one slow transfer does not hold up the jobs behind it. The first
// failure in input order is returned and no further job starts after it.
func runStagingJobs(ctx context.Context, count int, serial bool, run func(int) error) error {
	if count == 0 {
		return ctx.Err()
	}
	workers := min(installFileWorkers, count)
	if serial {
		workers = 1
	}
	var (
		mu     sync.Mutex
		next   int
		failed bool
		errs   = make([]error, count)
		group  sync.WaitGroup
	)
	worker := func() {
		for {
			mu.Lock()
			if failed || next >= count {
				mu.Unlock()
				return
			}
			index := next
			next++
			mu.Unlock()
			if err := run(index); err != nil {
				mu.Lock()
				errs[index], failed = err, true
				mu.Unlock()
				return
			}
		}
	}
	for range workers {
		group.Go(worker)
	}
	group.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return ctx.Err()
}
