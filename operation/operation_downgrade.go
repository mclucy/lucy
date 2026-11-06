package operation

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/artifactstore"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/resolve"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream/routing"
)

// DowngradeOptions selects an older published version for direct manifest
// roots. Every target moves by the same number of eligible versions.
type DowngradeOptions struct {
	// Steps counts eligible older versions to move: one selects the version
	// immediately older than the lock, two the next one, and so on. Zero means
	// one; negative values are invalid.
	Steps int
	// AcceptPackageConflicts reports package module and metadata conflicts as
	// warnings. Runtime-provided requirements and acquisition failures stay
	// fatal.
	AcceptPackageConflicts bool
	// Install carries the shared installation options. Locked and Offline are
	// rejected because a downgrade must resolve candidate history.
	Install install.Options
}

// Downgrade moves each requested direct root to an older published version,
// then resolves and installs the whole graph as one transaction. The runtime
// and the packages nobody requested stay at their locked versions where the
// dependency graph allows it.
func (s *Service) Downgrade(
	ctx context.Context,
	root string,
	args []string,
	ecosystem catalog.Ecosystem,
	opts DowngradeOptions,
) (result *install.Result, resultErr error) {
	steps := opts.Steps
	switch {
	case steps < 0:
		return nil, fmt.Errorf("downgrade steps must not be negative")
	case steps == 0:
		steps = 1
	}
	if len(args) == 0 {
		return nil, fmt.Errorf("downgrade requires at least one direct package")
	}
	if opts.Install.Locked {
		return nil, fmt.Errorf("downgrade cannot run with --locked; it must resolve an older version")
	}
	if opts.Install.Offline {
		return nil, fmt.Errorf("downgrade cannot run with --offline; candidate history requires network access")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root = absolute
	d, old, err := commandDocuments(root, s.logger)
	if err != nil {
		return nil, err
	}
	if old == nil {
		return nil, fmt.Errorf("lucy-lock.yaml is required; run lucy install first")
	}
	baseManifest, err := d.Fingerprint()
	if err != nil {
		return nil, err
	}
	if old.ManifestFingerprint != baseManifest {
		return nil, fmt.Errorf("manifest changed since the lock; run lucy install first")
	}
	baseLock, err := old.Fingerprint()
	if err != nil {
		return nil, err
	}
	lease, err := artifactstore.AcquireOperation()
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, lease.Close()) }()
	writer, err := workspacewrite.Lock(root, s.logger)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := filetrack.RecoverLocked(root, s.logger); err != nil {
		return nil, err
	}
	currentManifest, err := document.ManifestFile(root).Read()
	if err != nil {
		return nil, err
	}
	currentManifestFingerprint, err := currentManifest.Fingerprint()
	if err != nil {
		return nil, err
	}
	if currentManifestFingerprint != baseManifest {
		return nil, fmt.Errorf("manifest changed during downgrade planning; retry")
	}
	currentLock, err := document.LockFile(root).Read()
	if err != nil {
		return nil, err
	}
	currentLockFingerprint, err := currentLock.Fingerprint()
	if err != nil {
		return nil, err
	}
	if currentLockFingerprint != baseLock {
		return nil, fmt.Errorf("lock changed during downgrade planning; retry")
	}
	targets, err := s.selectDowngradeTargets(ctx, d, old, args, ecosystem, steps)
	if err != nil {
		return nil, err
	}
	materialize, fetch := s.install.Materializer(ctx, opts.Install)
	// The lock's core modules describe the frozen runtime. A lock that
	// predates recording them needs the artifact, and failing to read it
	// would make the resolution answer a different runtime.
	coreModules, err := routing.CoreModules(
		ctx,
		old.Server.Product,
		old.Server.Artifact,
		materialize,
	)
	if err != nil {
		return nil, fmt.Errorf("read server core modules: %w", err)
	}
	nextManifest, err := cloneManifest(d)
	if err != nil {
		return nil, err
	}
	var manifestChanges []install.ManifestChange
	for _, target := range targets {
		if err := setDowngradeSelector(nextManifest, target, target.Version); err != nil {
			return nil, err
		}
		if target.Requirement.Selector == target.Version {
			continue
		}
		manifestChanges = append(manifestChanges, install.ManifestChange{
			Reference:        target.Reference,
			PreviousSelector: target.Requirement.Selector,
			Selector:         target.Version,
		})
	}
	candidates, err := downgradeCandidateSource(s.source.Candidates, targets)
	if err != nil {
		return nil, err
	}
	packages, warnings, err := resolve.Packages(resolve.PackagesRequest{
		Context:                ctx,
		Manifest:               nextManifest,
		Previous:               old.Packages,
		Target:                 upgradeTarget(old, coreModules),
		Candidates:             candidates,
		Materialize:            materialize,
		WithOptional:           opts.Install.WithOptional,
		AcceptPackageConflicts: opts.AcceptPackageConflicts,
		ExpandDependencies:     true,
		Cache:                  resolve.NewAnalysisCache(),
		Logger:                 s.logger,
		Trace:                  upgradeResolveTrace(opts.Install.Trace),
	})
	if err != nil {
		return nil, err
	}
	if err := verifyDowngradeSelection(packages, targets); err != nil {
		return nil, err
	}
	if opts.Install.WithOptional {
		if err := recordResolvedRequirements(nextManifest, packages); err != nil {
			return nil, err
		}
	}
	if err := nextManifest.Normalize(); err != nil {
		return nil, err
	}
	requirements := rootRequirements(nextManifest)
	for i := range packages {
		for j := range packages[i].Requirements {
			binding := &packages[i].Requirements[j]
			requirement, ok := requirements[binding.Reference]
			if !ok {
				continue
			}
			binding.Selector = requirement.Selector
			binding.FilePattern = requirement.FilePattern
			binding.Enabled = requirement.IsEnabled()
		}
	}
	nextFingerprint, err := nextManifest.Fingerprint()
	if err != nil {
		return nil, err
	}
	resolved := &document.Lock{
		SchemaVersion:       document.LockSchemaVersion,
		ManifestFingerprint: nextFingerprint,
		Server:              old.Server,
		MCDR:                old.MCDR,
		Packages:            packages,
		Files:               slices.Clone(nextManifest.Files),
	}
	if err := resolved.Validate(); err != nil {
		return nil, err
	}
	publishManifest := true
	plan, err := install.PlanFromResolution(
		nextManifest,
		old,
		resolved,
		baseManifest,
		publishManifest,
	)
	if err != nil {
		return nil, err
	}
	plan.Result.Kind = install.ResultKind
	plan.Result.Reason = "package downgrade"
	plan.Result.ManifestChanges = manifestChanges
	plan.Result.Warnings = warnings
	plan.Result.Fetch = *fetch
	// ApplyPlan takes the writer lock itself and rechecks the fingerprints
	// this plan recorded, so the planning lock must be released first.
	if err := writer.Close(); err != nil {
		return nil, err
	}
	applyOpts := opts.Install
	applyOpts.Locked = false
	applyOpts.Offline = false
	applyOpts.AcceptBroken = false
	applyOpts.Overwrite = false
	applyOpts.ForceResolve = false
	applyOpts.RequirementChange = false
	applyOpts.PublishManifest = publishManifest
	return s.install.ApplyPlan(ctx, root, plan, applyOpts)
}

// recordResolvedRequirements makes optional dependencies that resolution
// installed explicit manifest requirements, matching Add's --with-optional
// behavior so the next resolution sees the same graph.
func recordResolvedRequirements(d *document.Manifest, packages []document.Package) error {
	for i := range packages {
		p := &packages[i]
		for _, requirement := range p.Requirements {
			recorded := document.Requirement{
				Selector:    requirement.Selector,
				FilePattern: requirement.FilePattern,
				Enabled:     new(requirement.Enabled),
			}
			if p.Environment == serverpkg.EnvironmentMCDR {
				if d.MCDR == nil {
					return fmt.Errorf("resolution selected MCDR package %s but the manifest has no MCDR runtime", p.ProjectID)
				}
				d.MCDR.Packages[requirement.Reference] = recorded
				continue
			}
			if d.Server.Packages[p.Ecosystem] == nil {
				d.Server.Packages[p.Ecosystem] = map[string]document.Requirement{}
			}
			d.Server.Packages[p.Ecosystem][requirement.Reference] = recorded
		}
	}
	return nil
}
