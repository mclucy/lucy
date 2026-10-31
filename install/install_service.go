package install

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/artifactstore"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/javaruntime"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/serverregistry"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
)

// Source is the routing capability installation consumes. Configured routing
// implements it; installation never selects a provider or transport on its
// own.
type Source interface {
	Candidates(
		ctx context.Context,
		registry catalog.Registry,
		project, minecraft string,
		ecosystem catalog.Ecosystem,
	) ([]upstream.Candidate, error)
	Metadata(ctx context.Context, endpoint string, target any) error
	ResolveServerProduct(
		ctx context.Context,
		request routing.ServerProductRequest,
		acquire func(*catalog.Artifact) (artifact.MaterializedFile, error),
	) (routing.ServerProductResolution, error)
}

// Service owns one installation pipeline: configured routing, the artifact
// cache, tracked-file provisioning, the Python host, and the diagnostic
// logger. Every entry point receives its root and request options explicitly.
type Service struct {
	source      Source
	network     *cache.Client
	tracker     *filetrack.Service
	python      *PythonHost
	registry    *serverregistry.Registry
	logger      *log.Logger
	githubProxy string
}

// NewService constructs the installation service from its explicit
// dependencies. network performs artifact transfers; tracker provisions
// tracked files; python supplies interpreter and process capabilities;
// registry records successful installations for the artifact store's
// retention sweep; githubProxy mirrors release asset downloads when
// non-empty.
func NewService(
	source Source,
	network *cache.Client,
	tracker *filetrack.Service,
	python *PythonHost,
	registry *serverregistry.Registry,
	logger *log.Logger,
	githubProxy string,
) *Service {
	return &Service{
		source:      source,
		network:     network,
		tracker:     tracker,
		python:      python,
		registry:    registry,
		logger:      logger,
		githubProxy: githubProxy,
	}
}

// registerServer records the workspace before any artifact enters the store.
// A successful installation therefore always has a registry entry whose lock
// the artifact store's retention sweep can read. Registration failure fails
// the installation instead of reporting success that the sweep cannot trace.
func (s *Service) registerServer(root string, result *Result) error {
	if s.registry == nil {
		return errors.New("server registry is unavailable; cannot record this installation for artifact retention")
	}
	duplicates, err := s.registry.RegisterInstallation(root)
	if err != nil {
		return fmt.Errorf("register server before installation: %w", err)
	}
	if result != nil {
		for _, duplicate := range duplicates {
			result.Warnings = append(result.Warnings, fmt.Errorf(
				"server name %q is also registered at %s; --server cannot select an ambiguous name",
				duplicate.Name,
				duplicate.Path,
			))
		}
	}
	return nil
}

// Resolve builds the lock document that answers d without staging or
// committing the server tree. It records the declared packages alone, because
// the workspace the manifest describes already holds a working set that
// catalogue dependencies must not extend. Artifact bytes still enter the
// artifact store because the lock pins their hashes. It returns the
// resolution facts alongside the lock. The caller must hold the workspace
// writer lock.
func (s *Service) Resolve(
	ctx context.Context,
	root string,
	d *document.Manifest,
	old *document.Lock,
	opts Options,
) (*document.Lock, *Result, error) {
	result := newResult()
	result.Mode = ModeResolve
	result.Reason = "lock resolved"
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, result, err
	}
	if d == nil {
		return nil, result, fmt.Errorf("manifest required to resolve a lock")
	}
	opts.declaredOnly = true
	if err := workspacewrite.SafePath(absolute, ".lucy"); err != nil {
		return nil, result, err
	}
	fingerprint, err := d.Fingerprint()
	if err != nil {
		return nil, result, err
	}
	if old != nil && !opts.WithOptional && old.ManifestFingerprint == fingerprint {
		s.logger.Debug("lock resolution reused current lock", "reason", "manifest unchanged")
		result.Mode = ModeReplay
		result.Reason = "manifest unchanged; lock reused"
		return old, result, nil
	}
	store := s.newDownloadStore(ctx, opts, result)
	resolved, err := s.resolveInstall(ctx, absolute, d, old, fingerprint, &opts, store, result)
	if err != nil {
		return nil, result, err
	}
	if err := resolved.Validate(); err != nil {
		return nil, result, err
	}
	return resolved, result, nil
}

// PlanInstall resolves a lock without staging or committing the server tree.
// It holds the artifact-store operation lease and the workspace writer lock
// only while reading and resolving, so a collection sweep cannot reclaim bytes
// the resolution materializes. Both locks are released when planning returns;
// the plan itself reserves no artifacts, and ApplyPlan acquires the lease
// again before using it.
func (s *Service) PlanInstall(
	ctx context.Context,
	root string,
	d *document.Manifest,
	old *document.Lock,
	opts Options,
) (plan *Plan, resultErr error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root = absolute
	lease, err := artifactstore.AcquireOperation()
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, lease.Close()) }()
	if err := workspacewrite.SafePath(root, ".lucy"); err != nil {
		return nil, err
	}
	writer, err := workspacewrite.Lock(root, s.logger)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	_, baseLock, err := preflightLocked(root, d, old, opts, false, s.logger)
	if err != nil {
		return nil, err
	}
	previous, err := recoveredLock(old, baseLock)
	if err != nil {
		return nil, err
	}
	result := newResult()
	store := s.newDownloadStore(ctx, opts, result)
	// The plan answers the recovered workspace: the previous lock is always the
	// recovered one, so a caller that read documents before recovery cannot
	// hand the resolver a stale base. The manifest stays the caller's document
	// because a publish plan carries an intended edit; ApplyPlan rejects a
	// non-publishing plan whose manifest does not match the recorded base.
	resolved, err := s.resolveLock(ctx, root, d, previous, &opts, store, result)
	if err != nil {
		return nil, err
	}
	result.PublishedManifest = opts.PublishManifest
	result.PublishedLock = opts.publishLock
	baseManifestFingerprint, baseLockFingerprint, err := workspaceFingerprints(root)
	if err != nil {
		return nil, err
	}
	return &Plan{
		Manifest:                d,
		Previous:                previous,
		Resolved:                resolved,
		PublishManifest:         opts.PublishManifest,
		PublishLock:             opts.publishLock,
		BaseManifestFingerprint: baseManifestFingerprint,
		BaseLockFingerprint:     baseLockFingerprint,
		Result:                  result,
	}, nil
}

// ApplyPlan stages and commits a previously resolved plan. It rechecks the
// workspace state before staging, so a plan cannot overwrite a concurrent
// manifest or lock edit.
func (s *Service) ApplyPlan(
	ctx context.Context,
	root string,
	plan *Plan,
	opts Options,
) (result *Result, resultErr error) {
	if plan == nil || plan.Resolved == nil {
		return nil, fmt.Errorf("installation plan is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root = absolute
	lease, err := artifactstore.AcquireOperation()
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, lease.Close()) }()
	if err := workspacewrite.SafePath(root, ".lucy"); err != nil {
		return nil, err
	}
	writer, err := workspacewrite.Lock(root, s.logger)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	preflightOptions := opts
	preflightOptions.PublishManifest = plan.PublishManifest
	if _, _, err := preflightLocked(root, plan.Manifest, plan.Previous, preflightOptions, true, s.logger); err != nil {
		return nil, err
	}
	if err := checkPlannedDocuments(plan); err != nil {
		return nil, err
	}
	if err := checkPlannedState(root, plan); err != nil {
		return nil, err
	}
	if err := s.registerServer(root, plan.Result); err != nil {
		return nil, err
	}
	// The isolated MCDReforged environment is keyed by its Python requirements,
	// so staging provisions a fresh environment whenever this list changes.
	if previous := plan.Previous; previous != nil && previous.MCDR != nil &&
		plan.Resolved.MCDR != nil {
		before := mcdrPythonRequirements(previous)
		after := mcdrPythonRequirements(plan.Resolved)
		if !slices.Equal(before, after) {
			if plan.Result == nil {
				plan.Result = newResult()
			}
			plan.Result.MCDR = &MCDRChange{
				Version:        plan.Resolved.MCDR.Version,
				Inputs:         len(after),
				PreviousInputs: len(before),
			}
		}
	}
	result = plan.Result
	if result == nil {
		result = newResult()
		result.applyChanges(plan.Previous, plan.Resolved, plan.Manifest)
	}
	store := s.newDownloadStore(ctx, opts, result)
	opts.PublishManifest = plan.PublishManifest
	opts.publishLock = plan.PublishLock
	diagnostics, err := s.stageInstall(ctx, root, plan.Manifest, plan.Resolved, store, opts, result)
	result.Diagnostics = diagnostics
	return result, err
}

// Sync converges the workspace to its manifest and lock. A missing manifest or
// lock is allowed only when the other document can drive the operation.
func (s *Service) Sync(
	ctx context.Context,
	root string,
	d *document.Manifest,
	old *document.Lock,
	opts Options,
) (*Result, error) {
	result := newResult()
	diagnostics, resultErr := s.syncInstall(ctx, root, d, old, opts, result)
	result.Diagnostics = diagnostics
	return result, resultErr
}

// Materializer returns a verified artifact materializer and the fetch summary
// it updates. Callers must hold the workspace writer lock while resolving.
func (s *Service) Materializer(
	ctx context.Context,
	opts Options,
) (func(*catalog.Artifact) (artifact.MaterializedFile, error), *FetchSummary) {
	summary := &FetchSummary{}
	store := &downloadStore{
		ctx:         ctx,
		offline:     opts.Offline,
		trace:       opts.Trace,
		fetch:       summary,
		network:     s.network,
		logger:      s.logger,
		githubProxy: s.githubProxy,
		injected:    opts.Materialize,
	}
	return store.materialize, summary
}

// CheckJava reports whether an installed Java runtime satisfies the lock. It
// never installs or modifies a runtime.
func (s *Service) CheckJava(
	ctx context.Context,
	root string,
	locked *document.Lock,
) (*javaruntime.Check, error) {
	return javaruntime.Select(ctx, root, javaruntime.RequirementOf(locked, s.logger), s.logger)
}
