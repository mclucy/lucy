package install

import (
	"context"
	"path/filepath"
	"sync"
	"time"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/fsutil"
)

const installFileWorkers = 4

// stagePackages validates ownership and destinations before acquiring or
// copying independent package artifacts.
func (s *Service) stagePackages(root, stage string, resolved *document.Lock, store *downloadStore, stagedOutputs map[string]bootstrap.Output, opts Options) (map[string]bool, error) {
	if err := rejectSharedDestinations(root, resolved, stagedOutputs); err != nil {
		return nil, err
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
			return nil, err
		}
		if opts.Overwrite {
			overwritePaths[filepath.ToSlash(destination)] = true
		}
		jobs = append(jobs, job{pkg: p, destination: destination})
	}
	if len(jobs) == 0 {
		return overwritePaths, nil
	}
	const phase = "package staging"
	started := time.Now()
	opts.Trace.phaseStarted(phase)
	var progress sync.Mutex
	completed := 0
	err := runStagingJobs(store.ctx, len(jobs), store.injected != nil, func(i int) error {
		j := jobs[i]
		source, err := store.materializeFor(&j.pkg.Artifact, filepath.ToSlash(j.destination))
		if err != nil {
			return err
		}
		j.pkg.Artifact.Hashes = source.Hashes
		if err := fsutil.AtomicCopyFile(source.Path, filepath.Join(stage, j.destination), 0o644); err != nil {
			return err
		}
		progress.Lock()
		completed++
		opts.Trace.ReportPhaseProgress(phase, completed, len(jobs))
		progress.Unlock()
		return nil
	})
	if err != nil {
		return nil, err
	}
	opts.Trace.phaseDone(PhaseInfo{Name: phase, Elapsed: time.Since(started), Count: len(jobs), Total: len(jobs)})
	s.logger.Debug("packages staged", "active", len(jobs), "recorded", len(resolved.Packages))
	return overwritePaths, nil
}

// runStagingJobs joins each bounded group before considering the next one, so
// the first error in input order is returned. Injected sources remain serial.
func runStagingJobs(ctx context.Context, count int, serial bool, run func(int) error) error {
	workers := installFileWorkers
	if serial {
		workers = 1
	}
	for base := 0; base < count; base += workers {
		if err := ctx.Err(); err != nil {
			return err
		}
		width := min(workers, count-base)
		if width == 1 {
			if err := run(base); err != nil {
				return err
			}
			continue
		}
		var results [installFileWorkers]error
		var group sync.WaitGroup
		for offset := range width {
			group.Go(func() { results[offset] = run(base + offset) })
		}
		group.Wait()
		for _, err := range results[:width] {
			if err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}
