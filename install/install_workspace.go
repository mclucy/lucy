package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/artifactstore"
	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/bootstrap/snapshot"
	"github.com/mclucy/lucy/cas"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/javaruntime"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
)

type (
	Options struct {
		Locked          bool
		Offline         bool
		AcceptBroken    bool
		Overwrite       bool
		PublishManifest bool
		WithOptional    bool
		// MCDRVenv creates an isolated MCDReforged environment instead of
		// requiring a global executable.
		MCDRVenv bool
		// ExpectedManifest pins the manifest fingerprint the caller selected
		// packages from. Preflight checks it after replaying interrupted
		// journals, so a concurrent or recovered edit fails instead of being
		// overwritten.
		ExpectedManifest string
		// ExpectedLock pins the lock fingerprint the caller planned from.
		// Preflight checks it against the recovered lock for the same reason.
		ExpectedLock string
		// RefreshBootstrap skips the bootstrap snapshot cache, so the next
		// install executes the method and publishes a fresh snapshot.
		RefreshBootstrap bool
		// ForceResolve resolves even when the manifest fingerprint matches the
		// lock. Upgrade uses it because a lock can answer the manifest while
		// still being older than the eligible versions.
		ForceResolve bool
		// RequirementChange records that the caller edited declared package
		// requirements, so a lock whose fingerprint no longer matches is
		// expected rather than stale.
		RequirementChange bool
		// declaredOnly records the manifest's packages without adding their
		// catalogue dependencies. Adoption sets it because the workspace
		// already holds a working set that resolution must not extend.
		declaredOnly bool
		// Adopted lists releases the workspace already holds. Resolution
		// selects them for their reference without a catalogue query, so a
		// locally valid artifact stays managed when upstream offers no release
		// for the target server.
		Adopted []AdoptedRelease
		// LocalRuntime lists the regular files of a selected observed runtime
		// with the digest and mode initialization recorded. Resolution may
		// reuse them only when the requested artifact's trusted digest proves
		// they are the same bytes. It never substitutes a file by name or role.
		LocalRuntime []AdoptedRuntimeFile
		publishLock  bool
		// SkipRegistration leaves the workspace unregistered. Staging callers
		// set it because a staged tree is not yet a server; they register the
		// workspace once its documents exist.
		SkipRegistration bool
		// Trace receives operation facts for the command layer to present.
		Trace *Trace
		// ProvisionJava runs before staging when the selected runtime misses
		// the requirement. It returns the installed home when ordinary
		// discovery cannot find it; selection then verifies the runtime again.
		ProvisionJava func(ctx context.Context, requirement javaruntime.Requirement, check *javaruntime.Check) (string, error)
		// Materialize, when non-nil, supplies artifact bytes for this
		// operation instead of the configured cache and network. The returned
		// path must exist; installation recomputes its digests and verifies
		// them against the artifact's expected hashes before use. Injected
		// bytes stay outside the operation's FetchSummary, which describes
		// transfers the configured cache performed.
		Materialize func(*catalog.Artifact) (artifact.MaterializedFile, error)
	}
)

func (s *Service) syncInstall(ctx context.Context, root string, d *document.Manifest, old *document.Lock, opts Options, result *Result) (diagnostics []filetrack.Diagnostic, resultErr error) {
	// Report before recovery, resolution, or replay so a failure or
	// cancellation still carries the workload this operation started from.
	opts.Trace.reportPackageCount(old)
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
	baseManifest, baseLock, documents, err := preflightLocked(root, d, old, opts, s.logger)
	if err != nil {
		return nil, err
	}
	if !opts.SkipRegistration {
		if err := s.registerServer(root, result); err != nil {
			return nil, err
		}
	}
	// Resolution and staging must start from the recovered documents. A
	// publishing caller still supplies the manifest it intends to write.
	manifest := baseManifest
	if opts.PublishManifest {
		manifest = d
	}
	store := s.newDownloadStore(ctx, opts, result)
	resolved, err := s.resolveLock(ctx, root, manifest, baseLock, &opts, store, result)
	if err != nil {
		return nil, err
	}
	return s.stageInstall(ctx, root, manifest, resolved, documents, store, opts, result)
}

// stageInstall materializes a resolved lock in the server directory and commits
// it with the manifest it was resolved from. Resolution paths differ in how they
// build the lock; every one of them lands here.
func (s *Service) stageInstall(
	ctx context.Context,
	root string,
	d *document.Manifest,
	resolved *document.Lock,
	documents WorkspaceDocuments,
	store *downloadStore,
	opts Options,
	result *Result,
) ([]filetrack.Diagnostic, error) {
	if err := filetrack.ValidateOwnership(root, resolved.Files, resolved); err != nil {
		return nil, err
	}
	check, err := s.selectJava(ctx, root, resolved, opts)
	if err != nil {
		return nil, err
	}
	result.Java = check
	bootstrapOK, current, err := installationCurrent(ctx, root, resolved, s.logger)
	if err != nil {
		return nil, err
	}
	// A refresh request executes the bootstrap method again to publish a
	// fresh snapshot.
	if opts.RefreshBootstrap {
		bootstrapOK, current = false, false
	}
	// Publishing documents alone does not require an available MCDR runtime.
	// An explicit venv request still reconciles the wrapper.
	metadataOnly := opts.PublishManifest || opts.publishLock
	reconcileMCDR := !metadataOnly || opts.MCDRVenv
	if current && (!reconcileMCDR || resolved.MCDR == nil ||
		mcdrWrapperCurrent(root, resolved, s.python, opts.MCDRVenv)) {
		s.logger.Info(
			"installation already current",
			"root", root,
			"packages", len(resolved.Packages),
		)
		if err := validateUnmanagedArtifacts(ctx, root, resolved, opts.Overwrite, s.logger); err != nil {
			return nil, err
		}
		if metadataOnly {
			var manifest *document.Manifest
			if opts.PublishManifest {
				manifest = d
			}
			// A current installation still publishes its documents through
			// the installation journal, so a concurrent edit is detected and
			// a partial manifest/lock publication rolls back.
			if _, err := applyCommit(
				root, "", manifest, resolved, opts.PublishManifest,
				nil, nil, nil, false, installCommitMetadataOnly, documents, s.logger,
			); err != nil {
				return nil, err
			}
			result.PublishedManifest = opts.PublishManifest
			result.PublishedLock = true
			return s.provisionCommitted(ctx, root, resolved, opts)
		}
		return s.tracker.Provision(ctx, root, resolved.Files, opts.Offline)
	}
	if current {
		s.logger.Debug(
			"reconciling MCDR wrapper through the installation transaction",
			"root", root,
		)
		if err := validateUnmanagedArtifacts(ctx, root, resolved, opts.Overwrite, s.logger); err != nil {
			return nil, err
		}
		stage, err := os.MkdirTemp(filepath.Join(root, ".lucy"), "stage-*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(stage)
		if err := preparePython(ctx, root, stage, resolved, store, s.python, opts.MCDRVenv); err != nil {
			return nil, err
		}
		var manifest *document.Manifest
		if opts.PublishManifest {
			manifest = d
		}
		mcdrOutputs := mcdrWrapperOutputs(resolved)
		counts, err := applyCommit(
			root, stage, manifest, resolved, opts.PublishManifest, nil,
			mcdrOutputs, mcdrOutputs, false, installCommitOverlay, documents, s.logger,
		)
		if err != nil {
			return nil, err
		}
		result.Files = counts
		result.PublishedManifest = opts.PublishManifest
		result.PublishedLock = opts.publishLock || opts.PublishManifest
		return s.provisionCommitted(ctx, root, resolved, opts)
	}
	s.logger.Debug("staging installation", "root", root, "reason", "installation missing or stale")
	opts.Trace.phaseStarted("install")
	start := time.Now()
	installed := false
	defer func() {
		if !installed {
			return
		}
		elapsed := time.Since(start)
		result.addPhase("install", elapsed)
		opts.Trace.phaseDone(PhaseInfo{
			Name:    "install",
			Elapsed: elapsed,
			Count:   result.Files.Created + result.Files.Replaced,
		})
	}()
	stage, err := os.MkdirTemp(filepath.Join(root, ".lucy"), "stage-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(stage)
	var bootstrapOutputs map[string]bootstrap.Output
	stagedBootstrap := !bootstrapOK
	if stagedBootstrap {
		// Bootstrap classification and package staging must not share a view
		// of the stage. StagedImage walks the server tree and accepts only the
		// method's declared outputs, so packages and wrapper files staged
		// alongside it would be rejected as undeclared runtime content.
		bootstrapOutputs, err = s.stageServerTree(
			ctx, stage, resolved, check.Executable(), store, opts,
		)
		if err != nil {
			return nil, err
		}
	} else {
		s.logger.Debug("bootstrap already current; skipping server tree staging", "root", root)
	}
	mcdrOutputs := mcdrWrapperOutputs(resolved)
	stagedOutputs := make(map[string]bootstrap.Output, len(bootstrapOutputs)+len(mcdrOutputs))
	for path, output := range bootstrapOutputs {
		stagedOutputs[path] = output
	}
	for path, output := range mcdrOutputs {
		for existing := range stagedOutputs {
			if !strings.EqualFold(existing, path) {
				continue
			}
			if existing == path ||
				workspacewrite.InstallPathsAlias(root, existing, path) {
				return nil, fmt.Errorf(
					"bootstrap output %s and MCDR wrapper output %s claim the same path",
					existing, path,
				)
			}
		}
		stagedOutputs[path] = output
	}
	overwritePaths, err := s.stagePackages(
		root, stage, resolved, store, stagedOutputs, opts,
	)
	if err != nil {
		return nil, err
	}
	if resolved.MCDR != nil {
		if err := preparePython(ctx, root, stage, resolved, store, s.python, opts.MCDRVenv); err != nil {
			return nil, err
		}
	}
	if err := validateUnmanagedArtifacts(ctx, root, resolved, opts.Overwrite, s.logger); err != nil {
		return nil, err
	}
	counts, err := applyCommit(
		root, stage, d, resolved, opts.PublishManifest,
		overwritePaths, stagedOutputs, mcdrOutputs, stagedBootstrap, installCommitReconcile, documents, s.logger,
	)
	if err != nil {
		return nil, err
	}
	result.Files = counts
	result.PublishedManifest = opts.PublishManifest
	result.PublishedLock = opts.publishLock || opts.PublishManifest
	installed = true
	return s.provisionCommitted(ctx, root, resolved, opts)
}

// stageServerTree materializes the locked core and inputs, then runs the
// bootstrap that prepares the server tree in stage. It returns the validated
// bootstrap outputs by workspace path. Validation happens here, while stage
// contains bootstrap material only: later staged packages and wrapper files
// are not part of the method's output contract and must not be inspected
// against it.
func (s *Service) stageServerTree(
	ctx context.Context,
	stage string,
	resolved *document.Lock,
	java string,
	store *downloadStore,
	opts Options,
) (map[string]bootstrap.Output, error) {
	if opts.Locked {
		if err := requirePinnedArtifact(resolved.Server.Artifact.Hashes, resolved.Server.Artifact.Filename); err != nil {
			return nil, err
		}
		for _, input := range resolved.Server.Inputs {
			if input.Embedded != "" {
				continue
			}
			if err := requirePinnedArtifact(input.Hashes, input.ProjectID); err != nil {
				return nil, err
			}
		}
	}
	// A lock that pins every digest names its recipe before acquisition, so a
	// stored snapshot restores without the build inputs. Otherwise the key
	// exists once acquisition attaches every store digest.
	key, keyErr := snapshot.Key(resolved.Server)
	pinnedKey := keyErr == nil
	var snapStore *artifactstore.Store
	defer func() {
		if snapStore != nil {
			snapStore.Close()
		}
	}()
	lookup := func() (map[string]bootstrap.Output, bool, error) {
		if keyErr != nil {
			s.logger.Debug("bootstrap snapshot unavailable", "reason", keyErr.Error())
			return nil, false, nil
		}
		if snapStore == nil {
			opened, err := openSnapshotStore()
			if err != nil {
				s.logger.Debug("bootstrap snapshot store unavailable", "reason", err.Error())
				return nil, false, nil
			}
			snapStore = opened
		}
		if opts.RefreshBootstrap {
			return nil, false, nil
		}
		return s.restoreServerSnapshot(ctx, snapStore, key, stage, resolved.Server, opts.Trace)
	}
	if pinnedKey {
		outputs, restored, err := lookup()
		if err != nil || restored {
			return outputs, err
		}
	}

	materialized := make([]artifact.MaterializedFile, 1+len(resolved.Server.Inputs))
	err := runStagingJobs(ctx, len(materialized), store.injected != nil, func(i int) error {
		a := &resolved.Server.Artifact
		label := a.Filename
		if i > 0 {
			input := &resolved.Server.Inputs[i-1]
			if input.Embedded != "" {
				return nil
			}
			a, label = input, input.Filename
		}
		file, err := store.materializeFor(a, label)
		materialized[i] = file
		return err
	})
	if err != nil {
		return nil, err
	}
	core := materialized[0]
	resolved.Server.Artifact.Hashes = core.Hashes
	inputs := make([]string, len(resolved.Server.Inputs))
	for i := range resolved.Server.Inputs {
		file := materialized[i+1]
		if file.Path != "" {
			resolved.Server.Inputs[i].Hashes = file.Hashes
			inputs[i] = file.Path
		}
	}
	// Acquisition can add a stronger digest than the lock pinned, and the key
	// names the strongest one. Publication and a second lookup use the key
	// that later runs with the enriched lock compute.
	if enriched, enrichedErr := snapshot.Key(resolved.Server); !pinnedKey ||
		(enrichedErr == nil && enriched != key) {
		key, keyErr = enriched, enrichedErr
		outputs, restored, err := lookup()
		if err != nil || restored {
			return outputs, err
		}
	}
	if err := bootstrap.PrepareLocked(
		ctx,
		stage,
		resolved.Server,
		core.Path,
		inputs,
		s.link,
		java,
		opts.Trace.processOutput(),
		opts.Trace.bootstrapProgress(),
	); err != nil {
		return nil, err
	}
	// Classify the dynamic result now, while the server tree is the only
	// content in stage. This emits the output policy and role the commit
	// uses, including discard and seed policy, without re-walking the tree
	// after packages and wrapper files join it.
	outputs, err := stagedServerOutputs(stage, resolved.Server)
	if err != nil {
		return nil, err
	}
	// Only the checked output of an executed recipe enters the snapshot store.
	if snapStore != nil {
		if err := publishBootstrapSnapshot(ctx, snapStore, key, stage, resolved.Server, outputs); err != nil {
			s.logger.Warn(fmt.Errorf(
				"cannot publish bootstrap snapshot %s: %w",
				key.Hex(), err,
			))
		}
	}
	s.logger.Debug(
		"server tree prepared",
		"stage", stage,
		"core", resolved.Server.Filename,
		"inputs", len(inputs),
		"outputs", len(outputs),
	)
	return outputs, nil
}

// restoreServerSnapshot restores the snapshot of key into stage and verifies
// it. A hit returns the restored outputs and runs no processor. A restore
// failure or a verification failure clears the server root for a cold stage,
// and a verification failure also drops the snapshot, so the next operation
// does not repeat it. Only a failure to clear the stage is returned.
func (s *Service) restoreServerSnapshot(
	ctx context.Context,
	snapStore *artifactstore.Store,
	key cas.Digest,
	stage string,
	server document.LockedServer,
	trace *Trace,
) (map[string]bootstrap.Output, bool, error) {
	started := time.Now()
	snap, restored, err := restoreBootstrapSnapshot(ctx, snapStore, key, stage, server, s.link, trace)
	if err != nil {
		s.logger.Warn(fmt.Errorf(
			"bootstrap snapshot restore failed for %s; staging from scratch: %w",
			key.Hex(), err,
		))
		return nil, false, clearServerRoot(stage, server)
	}
	if !restored {
		return nil, false, nil
	}
	outputs, err := restoredServerOutputs(stage, server, snap)
	if err == nil {
		s.logger.Info("bootstrap snapshot restored", "key", key.Hex(), "files", len(outputs))
		trace.phaseDone(PhaseInfo{
			Name:    "bootstrap snapshot",
			Count:   len(outputs),
			Elapsed: time.Since(started),
		})
		return outputs, true, nil
	}
	s.logger.Warn(fmt.Errorf(
		"restored bootstrap snapshot for %s failed verification; staging from scratch: %w",
		key.Hex(), err,
	))
	if err := clearServerRoot(stage, server); err != nil {
		return nil, false, err
	}
	if err := snapStore.RemoveSnapshot(key); err != nil {
		s.logger.Warn(fmt.Errorf("cannot drop failed bootstrap snapshot %s: %w", key.Hex(), err))
	}
	return nil, false, nil
}

// restoredServerOutputs verifies a tree restored from a snapshot and indexes
// its entries by workspace path. A snapshot holds only the runtime zone and
// the seed zone, so the comparison proves those files rather than re-running
// the staging contract.
func restoredServerOutputs(
	stage string,
	server document.LockedServer,
	snap snapshot.Snapshot,
) (map[string]bootstrap.Output, error) {
	root, err := serverRootPath(stage, server)
	if err != nil {
		return nil, err
	}
	comparison, err := snapshot.Compare(root, snap.Entries)
	if err != nil {
		return nil, err
	}
	if !comparison.Complete() {
		return nil, fmt.Errorf(
			"restored bootstrap tree has %d differing, %d permission-differing, and %d missing files",
			len(comparison.Differing), len(comparison.OtherPermissions), len(comparison.Missing),
		)
	}
	outputs := make(map[string]bootstrap.Output, len(snap.Entries))
	for _, entry := range snap.Entries {
		path := filepath.ToSlash(filepath.Join(server.Directory, filepath.FromSlash(entry.Path)))
		outputs[path] = bootstrap.Output{
			Path:       path,
			ServerPath: entry.Path,
			SHA256:     entry.SHA256,
			Mode:       entry.Mode,
			Policy:     bootstrap.OutputPolicy(entry.Policy),
			Role:       bootstrap.MaterialRole(entry.Role),
		}
	}
	return outputs, nil
}

// stagedServerOutputs classifies the current staging tree against the locked
// bootstrap contract and indexes the outputs by workspace path.
func stagedServerOutputs(stage string, server document.LockedServer) (map[string]bootstrap.Output, error) {
	stagedOutputs, err := bootstrap.StagedImage(stage, server)
	if err != nil {
		return nil, err
	}
	return indexServerOutputs(stagedOutputs), nil
}

func indexServerOutputs(stagedOutputs []bootstrap.Output) map[string]bootstrap.Output {
	outputs := make(map[string]bootstrap.Output, len(stagedOutputs))
	for _, output := range stagedOutputs {
		outputs[output.Path] = output
	}
	return outputs
}

// clearServerRoot empties the server directory inside a private staging root.
// A failed snapshot restore or verification leaves partial content that must
// not merge with the cold stage that follows.
func clearServerRoot(stage string, server document.LockedServer) error {
	root, err := serverRootPath(stage, server)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if err := os.RemoveAll(filepath.Join(root, entry.Name())); err != nil {
			return err
		}
	}
	return nil
}

func requirePinnedArtifact(hashes map[string]string, name string) error {
	hasDigest := false
	for algorithm, digest := range hashes {
		switch algorithm {
		case "sha256", "sha512", "sha1", "md5":
		default:
			return fmt.Errorf("locked artifact %s has unsupported digest %s", name, algorithm)
		}
		hasDigest = hasDigest || strings.TrimSpace(digest) != ""
	}
	if hasDigest {
		return nil
	}
	return fmt.Errorf("locked artifact %s has no pinned digest", name)
}

// mcdrWrapperOutputs declares the files writeMCDRWrapper stages for an
// installation with an MCDReforged wrapper. They are not part of the server
// bootstrap method's contract, so they are classified here by exact path: the
// generated config is seeded only when the workspace has none, and the
// launcher script is managed. Filename heuristics cannot express that
// distinction once bootstrap and wrapper outputs share a stage.
func mcdrWrapperOutputs(resolved *document.Lock) map[string]bootstrap.Output {
	if resolved == nil || resolved.MCDR == nil {
		return nil
	}
	directory := resolved.MCDR.Directory
	config := filepath.ToSlash(filepath.Join(directory, "config.yml"))
	launcher := filepath.ToSlash(filepath.Join(directory, "run-mcdr.sh"))
	return map[string]bootstrap.Output{
		config: {
			Path:   config,
			Policy: bootstrap.PolicySeed,
			Role:   bootstrap.RoleRuntime,
		},
		launcher: {
			Path:   launcher,
			Policy: bootstrap.PolicyManaged,
			Role:   bootstrap.RoleRuntime,
		},
	}
}

func isFloating(v version.Version) bool {
	switch v {
	case "", version.Stable, version.Beta, version.Any:
		return true
	}
	return v == "latest"
}

// coreReusable reports whether a lock's server record answers the
// manifest's core request. Resolution reuses the pin when it does; a locked
// mutation treats a negative answer as a stale lock, because rebinding
// requirements must never re-target the server.
func (s *Service) coreReusable(ctx context.Context, d *document.Manifest, old *document.Lock, offline bool) (bool, error) {
	request := upstream.ServerProductRequest{
		Minecraft:      d.Server.Minecraft,
		Product:        d.Server.Core.Product,
		ProductVersion: d.Server.Core.ProductVersion,
		Repository:     d.Server.Core.Repository,
		FilePattern:    d.Server.Core.FilePattern,
	}
	if old.Server.Directory != d.Server.Directory {
		s.logger.Debug(
			"core pin not reusable",
			"reason", "server directory changed",
			"locked", old.Server.Directory,
			"requested", d.Server.Directory,
		)
		return false, nil
	}
	if old.Server.Minecraft != request.Minecraft || old.Server.Product != request.Product {
		s.logger.Debug(
			"core pin not reusable",
			"reason", "pinned minecraft or product changed",
			"minecraft", old.Server.Minecraft,
			"requested-minecraft", request.Minecraft,
			"product", old.Server.Product,
			"requested-product", request.Product,
		)
		return false, nil
	}
	if !isFloating(request.ProductVersion) && old.Server.CoreVersion != request.ProductVersion {
		s.logger.Debug(
			"core pin not reusable",
			"reason", "core version changed",
			"locked", old.Server.CoreVersion,
			"requested", request.ProductVersion,
		)
		return false, nil
	}
	// Dropping the repository override retargets the server at its official
	// distribution, so a locked GitHub artifact no longer satisfies the request
	// even though every pinned field still matches.
	if request.Repository == "" && old.Server.Provider == catalog.ProviderGitHub {
		s.logger.Debug(
			"core pin not reusable",
			"reason", "repository override dropped while pin came from a GitHub fork",
		)
		return false, nil
	}
	if request.Repository != "" {
		if offline {
			s.logger.Debug(
				"core pin not reusable",
				"reason", "repository override needs upstream confirmation while offline",
				"repository", request.Repository,
			)
			return false, nil
		}
		var repository struct {
			ID int64 `json:"id"`
		}
		if err := s.source.Metadata(ctx, "https://api.github.com/repos/"+request.Repository, &repository); err != nil {
			return false, err
		}
		if old.Server.Provider != catalog.ProviderGitHub || old.Server.ProjectID != strconv.FormatInt(repository.ID, 10) {
			s.logger.Debug(
				"core pin not reusable",
				"reason", "repository identity changed",
				"requested-repository", request.Repository,
				"resolved-id", repository.ID,
				"locked-project", old.Server.ProjectID,
			)
			return false, nil
		}
	}
	if request.FilePattern != "" {
		matched, err := filepath.Match(request.FilePattern, old.Server.Filename)
		if err != nil {
			return false, err
		}
		if !matched {
			s.logger.Debug(
				"core pin not reusable",
				"reason", "artifact filename no longer matches pattern",
				"locked", old.Server.Filename,
				"pattern", request.FilePattern,
			)
			return false, nil
		}
	}
	return true, nil
}

// rejectSharedDestinations enforces the lock's installability guarantee: a
// destination two active packages both claim, two destinations the target
// filesystem resolves to one entry, or a package destination that claims a
// bootstrap or wrapper output must fail before anything is written. Left
// unguarded the later copy silently replaces the earlier one, so the workspace
// diverges from a lock that still reports itself as current, and the commit
// would attribute the surviving bytes to the wrong origin.
func rejectSharedDestinations(
	root string,
	resolved *document.Lock,
	stagedOutputs map[string]bootstrap.Output,
) error {
	outputs := make(map[string][]bootstrap.Output, len(stagedOutputs))
	for _, output := range stagedOutputs {
		key := strings.ToLower(output.Path)
		outputs[key] = append(outputs[key], output)
	}
	type claim struct {
		path     string
		identity string
	}
	claimed := make(map[string][]claim)
	for i := range resolved.Packages {
		p := &resolved.Packages[i]
		if !p.Enabled() {
			continue
		}
		destination, err := resolved.PackageDestination(p)
		if err != nil {
			return err
		}
		destination = filepath.ToSlash(filepath.Clean(destination))
		identity := p.Artifact.Provider.String() + ":" + p.Artifact.ProjectID
		key := strings.ToLower(destination)
		for _, output := range outputs[key] {
			if output.Path == destination {
				return fmt.Errorf(
					"package %s installs to %s, which bootstrap already produces",
					identity, destination,
				)
			}
			if workspacewrite.InstallPathsAlias(root, output.Path, destination) {
				return fmt.Errorf(
					"package %s installs to %s, which aliases bootstrap output %s",
					identity, destination, output.Path,
				)
			}
		}
		for _, prior := range claimed[key] {
			if prior.path == destination {
				return fmt.Errorf(
					"packages %s and %s both install to %s",
					prior.identity, identity, destination,
				)
			}
			if workspacewrite.InstallPathsAlias(root, prior.path, destination) {
				return fmt.Errorf(
					"packages %s and %s install to aliasing destinations %s and %s",
					prior.identity, identity, prior.path, destination,
				)
			}
		}
		claimed[key] = append(claimed[key], claim{
			path:     destination,
			identity: identity,
		})
	}
	return nil
}

// provisionCommitted provisions tracked files after the lock is published. A
// failure here leaves the committed installation in place, so it is marked.
func (s *Service) provisionCommitted(ctx context.Context, root string, resolved *document.Lock, opts Options) ([]filetrack.Diagnostic, error) {
	diagnostics, err := s.tracker.Provision(ctx, root, resolved.Files, opts.Offline)
	if err != nil {
		err = &CommittedError{Err: err}
	}
	return diagnostics, err
}
