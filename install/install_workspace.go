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
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/internal/fsutil"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/javaruntime"
	"github.com/mclucy/lucy/receipt"
	"github.com/mclucy/lucy/server"
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
		publishLock  bool
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
	baseManifest, baseLock, err := preflightLocked(root, d, old, opts, true, s.logger)
	if err != nil {
		return nil, err
	}
	if err := s.registerServer(root, result); err != nil {
		return nil, err
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
	return s.stageInstall(ctx, root, manifest, resolved, store, opts, result)
}

// stageInstall materializes a resolved lock in the server directory and commits
// it with the manifest it was resolved from. Resolution paths differ in how they
// build the lock; every one of them lands here.
func (s *Service) stageInstall(
	ctx context.Context,
	root string,
	d *document.Manifest,
	resolved *document.Lock,
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
	current, err := installationCurrent(root, resolved, s.logger)
	if err != nil {
		return nil, err
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
				nil, nil, nil, false, installCommitMetadataOnly, s.logger,
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
			mcdrOutputs, mcdrOutputs, false, installCommitOverlay, s.logger,
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
	existingReceipt, err := receipt.Read(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	bCurrent, err := bootstrapCurrent(root, resolved, existingReceipt, s.logger)
	if err != nil {
		return nil, err
	}
	var bootstrapOutputs map[string]bootstrap.Output
	stagedBootstrap := !bCurrent
	if stagedBootstrap {
		// Bootstrap validation and package staging must not share a view of
		// the stage. InspectStage walks the server tree and accepts only the
		// method's declared outputs, so packages and wrapper files staged
		// alongside it would be rejected as unpermitted runtime content.
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
		overwritePaths, stagedOutputs, mcdrOutputs, stagedBootstrap, installCommitReconcile, s.logger,
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
			if err := requirePinnedArtifact(input.Artifact.Hashes, input.Path); err != nil {
				return nil, err
			}
		}
	}
	core, err := store.materialize(&resolved.Server.Artifact)
	if err != nil {
		return nil, err
	}
	resolved.Server.Artifact.Hashes = core.Hashes
	inputs := map[string]string{}
	for i := range resolved.Server.Inputs {
		input := &resolved.Server.Inputs[i]
		if input.Embedded != "" {
			continue
		}
		materialized, err := store.materialize(&input.Artifact)
		if err != nil {
			return nil, err
		}
		input.Artifact.Hashes = materialized.Hashes
		inputs[input.Path] = materialized.Path
	}
	if err := bootstrap.PrepareLocked(
		ctx,
		stage,
		resolved.Server,
		core.Path,
		inputs,
		java,
		opts.Trace.processOutput(),
	); err != nil {
		return nil, err
	}
	// Verify the dynamic result now, while the server tree is the only
	// content in stage. This emits the output classification the commit
	// uses, including discard and seed policy, without re-walking the tree
	// after packages and wrapper files join it.
	stagedOutputs, err := bootstrap.InspectStage(stage, resolved.Server)
	if err != nil {
		return nil, err
	}
	outputs := make(map[string]bootstrap.Output, len(stagedOutputs))
	for _, output := range stagedOutputs {
		outputs[output.Path] = output
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

// stagePackages copies every enabled package into stage after rejecting
// destinations two packages both claim or a package shares with a staged
// bootstrap or wrapper output. It returns the destinations an overwrite may
// replace.
func (s *Service) stagePackages(
	root string,
	stage string,
	resolved *document.Lock,
	store *downloadStore,
	stagedOutputs map[string]bootstrap.Output,
	opts Options,
) (map[string]bool, error) {
	if err := rejectSharedDestinations(root, resolved, stagedOutputs); err != nil {
		return nil, err
	}
	staged := 0
	overwritePaths := map[string]bool{}
	for i := range resolved.Packages {
		p := &resolved.Packages[i]
		if !p.Enabled() {
			continue
		}
		destination, err := packageDestination(resolved, p)
		if err != nil {
			return nil, err
		}
		if opts.Overwrite {
			overwritePaths[filepath.ToSlash(destination)] = true
		}
		source, err := store.materialize(&p.Artifact)
		if err != nil {
			return nil, err
		}
		p.Artifact.Hashes = source.Hashes
		if err := fsutil.AtomicCopyFile(source.Path, filepath.Join(stage, destination), 0o644); err != nil {
			return nil, err
		}
		staged++
	}
	s.logger.Debug("packages staged", "active", staged, "recorded", len(resolved.Packages))
	return overwritePaths, nil
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
		destination, err := packageDestination(resolved, p)
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

// packageFolder returns the directory that serves a package's environment and
// ecosystem.
func packageFolder(environment server.Environment, ecosystem catalog.Ecosystem) string {
	if environment == server.EnvironmentMCDR {
		return "plugins"
	}
	switch ecosystem {
	case catalog.EcosystemBukkit, catalog.EcosystemPaper:
		return "plugins"
	default:
		return "mods"
	}
}

// packageDestination reports where an enabled package lands inside the staged
// server tree.
func packageDestination(resolved *document.Lock, p *document.Package) (string, error) {
	directory := resolved.Server.Directory
	folder := packageFolder(p.Environment, p.Ecosystem)
	if p.Environment == server.EnvironmentMCDR {
		if resolved.MCDR == nil {
			return "", fmt.Errorf("MCDR package has no wrapper runtime")
		}
		directory = resolved.MCDR.Directory
	}
	return filepath.Join(directory, folder, p.Filename), nil
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
