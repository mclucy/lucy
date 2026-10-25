package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/javaruntime"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
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
		// packages from, so a concurrent edit fails instead of being
		// overwritten.
		ExpectedManifest string
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
		// ProvisionJava runs before staging when the selected Java runtime
		// misses the requirement of the lock. It must leave a runtime installed
		// or fail; the runtime is selected again afterwards.
		ProvisionJava func(ctx context.Context, requirement javaruntime.Requirement, check *javaruntime.Check) error
	}
)

func Sync(ctx context.Context, root string, d *manifest.Document, old *lockfile.Document, opts Options) (*Result, error) {
	result := newResult()
	diagnostics, resultErr := syncInstall(ctx, root, d, old, opts, result)
	result.Diagnostics = diagnostics
	return result, resultErr
}

func syncInstall(ctx context.Context, root string, d *manifest.Document, old *lockfile.Document, opts Options, result *Result) (diagnostics []filetrack.Diagnostic, resultErr error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	root = absolute
	if err := workspacewrite.SafePath(root, ".lucy"); err != nil {
		return nil, err
	}
	if d == nil && old == nil {
		return nil, fmt.Errorf("lucy.yaml or lucy-lock.yaml is required")
	}
	writer, err := workspacewrite.Lock(root)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := workspacewrite.RecoverInstall(root); err != nil {
		return nil, err
	}
	serverDirectory := ""
	if d != nil {
		serverDirectory = d.Server.Directory
	} else if old != nil {
		serverDirectory = old.Server.Directory
	}
	if workspace.RunningAt(filepath.Join(root, serverDirectory)) ||
		(serverDirectory != "." && workspace.RunningAt(root)) {
		return nil, fmt.Errorf("stop the running Minecraft server before installation")
	}
	if err := workspacewrite.RecoverInstall(root); err != nil {
		return nil, err
	}
	if opts.ExpectedManifest != "" {
		current, err := manifest.Read(root)
		if err != nil {
			return nil, err
		}
		fingerprint, err := manifest.Fingerprint(current)
		if err != nil {
			return nil, err
		}
		if fingerprint != opts.ExpectedManifest {
			return nil, fmt.Errorf("manifest changed during package selection; retry from current intent")
		}
	}
	store := &downloadStore{
		ctx:     ctx,
		dir:     filepath.Join(root, ".lucy", "downloads"),
		offline: opts.Offline,
		trace:   opts.Trace,
		fetch:   &result.Fetch,
	}
	resolved := old
	fingerprint := ""
	if d != nil {
		var err error
		fingerprint, err = manifest.Fingerprint(d)
		if err != nil {
			return nil, err
		}
	}
	switch {
	case old == nil:
		result.Mode = ModeResolve
		result.Reason = "no lock file to replay"
	case opts.WithOptional:
		result.Mode = ModeResolve
		result.Reason = "optional packages requested"
	case d != nil && old.ManifestFingerprint != fingerprint:
		result.Mode = ModeResolve
		result.Reason = "manifest changed since lock"
	default:
		result.Mode = ModeReplay
		result.Reason = "manifest unchanged; lock replays exactly"
	}
	log.Debug(
		"installation decision",
		"mode", string(result.Mode),
		"reason", result.Reason,
	)
	if old == nil || opts.WithOptional || (d != nil && old.ManifestFingerprint != fingerprint) {
		if opts.Locked {
			if !opts.RequirementChange || old == nil || d == nil {
				return nil, fmt.Errorf("locked installation requires a current lucy-lock.yaml")
			}
			reusable, err := coreReusable(ctx, d, old, opts.Offline)
			if err != nil {
				return nil, err
			}
			if !reusable {
				return nil, fmt.Errorf("manifest core no longer matches lucy-lock.yaml; install without --locked")
			}
			log.Debug(
				"locked installation reuses core",
				"reason", "manifest core still matches lock; requirements rebind in place",
			)
			resolved, err = mutateLock(d, old, fingerprint)
			if err != nil {
				return nil, err
			}
			if err := resolved.Validate(); err != nil {
				return nil, err
			}
			result.Mode = ModeLocked
			result.Reason = "manifest core matches; requirements rebind in place"
			opts.publishLock = true
			result.applyChanges(old, resolved, d)
			return stageInstall(ctx, root, d, resolved, store, opts, result)
		}
		resolved, err = resolveInstall(ctx, root, d, old, fingerprint, &opts, store, result)
		if err != nil {
			return nil, err
		}
	}
	if err := resolved.Validate(); err != nil {
		return nil, err
	}
	opts.publishLock = resolved != old
	result.applyChanges(old, resolved, d)
	return stageInstall(ctx, root, d, resolved, store, opts, result)
}

// stageInstall materializes a resolved lock in the server directory and commits
// it with the manifest it was resolved from. Resolution paths differ in how they
// build the lock; every one of them lands here.
func stageInstall(
	ctx context.Context,
	root string,
	d *manifest.Document,
	resolved *lockfile.Document,
	store *downloadStore,
	opts Options,
	result *Result,
) ([]filetrack.Diagnostic, error) {
	if err := filetrack.ValidateOwnership(root, resolved.Files, resolved); err != nil {
		return nil, err
	}
	check, err := selectJava(ctx, root, resolved, opts)
	if err != nil {
		return nil, err
	}
	result.Java = check
	current, err := installationCurrent(root, resolved)
	if err != nil {
		return nil, err
	}
	if current && !opts.PublishManifest {
		log.Info(
			"installation already current",
			"root", root,
			"packages", len(resolved.Packages),
		)
		if err := validateUnmanagedArtifacts(ctx, root, resolved, opts.Overwrite); err != nil {
			return nil, err
		}
		if resolved.MCDR != nil {
			if err := ensureMCDRRuntime(ctx, root, resolved, store, opts.MCDRVenv); err != nil {
				return nil, err
			}
		}
		if opts.publishLock {
			if err := lockfile.Write(root, resolved); err != nil {
				return nil, err
			}
			result.PublishedLock = true
			return provisionCommitted(ctx, root, resolved, opts)
		}
		return filetrack.Provision(ctx, root, resolved.Files, opts.Offline)
	}
	reason := "installation missing or stale"
	if current {
		reason = "manifest publication forces re-staging"
	}
	log.Debug("staging installation", "root", root, "reason", reason)
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
		check.Executable(),
		opts.Trace.processOutput(),
	); err != nil {
		return nil, err
	}
	log.Debug(
		"server tree prepared",
		"stage", stage,
		"core", resolved.Server.Filename,
		"inputs", len(inputs),
	)
	if err := rejectSharedDestinations(resolved); err != nil {
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
		if err := bootstrap.CopyLockedFile(source.Path, filepath.Join(stage, destination), 0o644); err != nil {
			return nil, err
		}
		staged++
	}
	log.Debug("packages staged", "active", staged, "recorded", len(resolved.Packages))
	if resolved.MCDR != nil {
		if err := preparePython(ctx, root, stage, resolved, store, opts.MCDRVenv); err != nil {
			return nil, err
		}
	}
	if err := validateUnmanagedArtifacts(ctx, root, resolved, opts.Overwrite); err != nil {
		return nil, err
	}
	counts, err := applyCommit(root, stage, d, resolved, opts.PublishManifest, overwritePaths)
	if err != nil {
		return nil, err
	}
	result.Files = counts
	result.PublishedManifest = opts.PublishManifest
	result.PublishedLock = opts.publishLock || opts.PublishManifest
	installed = true
	return provisionCommitted(ctx, root, resolved, opts)
}

func isFloating(v version.Version) bool {
	switch v {
	case "", version.Stable, version.Beta, version.Any:
		return true
	}
	return v == "latest"
}

// coreReusable reports whether a lock's server record still answers the
// manifest's core request. Resolution reuses the pin when it does; a locked
// mutation treats a negative answer as a stale lock, because rebinding
// requirements must never re-target the server.
func coreReusable(ctx context.Context, d *manifest.Document, old *lockfile.Document, offline bool) (bool, error) {
	request := routing.ServerProductRequest{
		Minecraft:      d.Server.Minecraft,
		Product:        d.Server.Core.Product,
		ProductVersion: d.Server.Core.ProductVersion,
		Repository:     d.Server.Core.Repository,
		FilePattern:    d.Server.Core.FilePattern,
	}
	if old.Server.Directory != d.Server.Directory {
		log.Debug(
			"core pin not reusable",
			"reason", "server directory changed",
			"locked", old.Server.Directory,
			"requested", d.Server.Directory,
		)
		return false, nil
	}
	if old.Server.Minecraft != request.Minecraft || old.Server.Product != request.Product {
		log.Debug(
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
		log.Debug(
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
		log.Debug(
			"core pin not reusable",
			"reason", "repository override dropped while pin came from a GitHub fork",
		)
		return false, nil
	}
	if request.Repository != "" {
		if offline {
			log.Debug(
				"core pin not reusable",
				"reason", "repository override needs upstream confirmation while offline",
				"repository", request.Repository,
			)
			return false, nil
		}
		var repository struct {
			ID int64 `json:"id"`
		}
		if err := routing.Metadata(ctx, "https://api.github.com/repos/"+request.Repository, &repository); err != nil {
			return false, err
		}
		if old.Server.Provider != catalog.ProviderGitHub || old.Server.ProjectID != strconv.FormatInt(repository.ID, 10) {
			log.Debug(
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
			log.Debug(
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
// destination two active packages both claim must fail before anything is
// written. Left unguarded the later copy silently replaces the earlier one, so
// the workspace diverges from a lock that still reports itself as current.
func rejectSharedDestinations(resolved *lockfile.Document) error {
	claimed := map[string]string{}
	for i := range resolved.Packages {
		p := &resolved.Packages[i]
		if !p.Enabled() {
			continue
		}
		destination, err := packageDestination(resolved, p)
		if err != nil {
			return err
		}
		identity := p.Artifact.Provider.String() + ":" + p.Artifact.ProjectID
		if claimedBy, taken := claimed[destination]; taken {
			return fmt.Errorf("packages %s and %s both install to %s", claimedBy, identity, destination)
		}
		claimed[destination] = identity
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
func packageDestination(resolved *lockfile.Document, p *lockfile.Package) (string, error) {
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
func provisionCommitted(ctx context.Context, root string, resolved *lockfile.Document, opts Options) ([]filetrack.Diagnostic, error) {
	diagnostics, err := filetrack.Provision(ctx, root, resolved.Files, opts.Offline)
	if err != nil {
		err = &CommittedError{Err: err}
	}
	return diagnostics, err
}
