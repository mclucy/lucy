package install

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/workspace"
)

type (
	Options struct {
		Locked          bool
		Offline         bool
		PublishManifest bool
		WithOptional    bool
		// ExpectedManifest pins the manifest fingerprint the caller selected
		// packages from, so a concurrent edit fails instead of being
		// overwritten.
		ExpectedManifest string
		// RequirementChange records that the caller edited declared package
		// requirements, so a lock whose fingerprint no longer matches is
		// expected rather than stale.
		RequirementChange bool
	}
	InitOptions struct {
		Minecraft, Distribution, CoreVersion, Repository, MCDRVersion, Directory string
		Force, AllowEmpty, Detect                                                bool
		// PromptMinecraft asks for a concrete Minecraft version when
		// initialization lacks one and returns the trimmed response. Nil keeps
		// the concrete-version-required error and never prompts.
		PromptMinecraft func() (string, error)
	}
)

func Sync(ctx context.Context, root string, d *manifest.Document, old *lockfile.Document, opts Options) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	root = absolute
	if err := SafeOutputPath(root, ".lucy"); err != nil {
		return err
	}
	if d == nil && old == nil {
		return fmt.Errorf("lucy.yaml or lucy-lock.yaml is required")
	}
	if err := os.MkdirAll(filepath.Join(root, ".lucy"), 0o755); err != nil {
		return err
	}
	writer, err := LockWorkspace(root)
	if err != nil {
		return err
	}
	defer writer.Close()
	serverDirectory := ""
	if d != nil {
		serverDirectory = d.Server.Directory
	} else if old != nil {
		serverDirectory = old.Server.Directory
	}
	if workspace.RunningAt(filepath.Join(root, serverDirectory)) {
		return fmt.Errorf("stop the running Minecraft server before installation")
	}
	if err := rollbackInterruptedCommit(root); err != nil {
		return err
	}
	if opts.ExpectedManifest != "" {
		current, err := manifest.Read(root)
		if err != nil {
			return err
		}
		fingerprint, err := manifest.Fingerprint(current)
		if err != nil {
			return err
		}
		if fingerprint != opts.ExpectedManifest {
			return fmt.Errorf("manifest changed during package selection; retry from current intent")
		}
	}
	store := &downloadStore{
		ctx:     ctx,
		dir:     filepath.Join(root, ".lucy", "downloads"),
		offline: opts.Offline,
	}
	resolved := old
	fingerprint := ""
	if d != nil {
		var err error
		fingerprint, err = manifest.Fingerprint(d)
		if err != nil {
			return err
		}
	}
	switch {
	case old == nil:
		log.Debug("installation decision", "mode", "resolve", "reason", "no lock file to replay")
	case opts.WithOptional:
		log.Debug("installation decision", "mode", "resolve", "reason", "optional packages requested")
	case d != nil && old.ManifestFingerprint != fingerprint:
		log.Debug(
			"installation decision",
			"mode", "resolve",
			"reason", "manifest changed since lock",
			"manifest", fingerprint,
			"lock", old.ManifestFingerprint,
		)
	default:
		log.Debug(
			"installation decision",
			"mode", "replay",
			"reason", "manifest unchanged; lock replays exactly",
			"manifest", fingerprint,
		)
	}
	if old == nil || opts.WithOptional || (d != nil && old.ManifestFingerprint != fingerprint) {
		if opts.Locked {
			if !opts.RequirementChange || old == nil || d == nil {
				return fmt.Errorf("locked installation requires a current lucy-lock.yaml")
			}
			reusable, err := coreReusable(ctx, d, old, opts.Offline)
			if err != nil {
				return err
			}
			if !reusable {
				return fmt.Errorf("manifest core no longer matches lucy-lock.yaml; install without --locked")
			}
			log.Debug(
				"locked installation reuses core",
				"reason", "manifest core still matches lock; requirements rebind in place",
			)
			resolved, err = mutateLock(d, old, fingerprint)
			if err != nil {
				return err
			}
			if err := resolved.Validate(); err != nil {
				return err
			}
			return stageInstall(ctx, root, d, resolved, store, opts)
		}
		if d == nil {
			return fmt.Errorf("manifest required to resolve installation")
		}
		if opts.Offline {
			return fmt.Errorf("offline resolution requires a current lock; metadata selection is unavailable")
		}
		log.Info(
			"resolving installation",
			"minecraft", d.Server.Minecraft,
			"product", d.Server.Core.Product,
			"core", d.Server.Core.ProductVersion,
			"mcdr", d.MCDR != nil,
		)
		coreReq := types.ServerProductRequest{
			Minecraft:      d.Server.Minecraft,
			Product:        d.Server.Core.Product,
			ProductVersion: d.Server.Core.ProductVersion,
			Repository:     d.Server.Core.Repository,
			FilePattern:    d.Server.Core.FilePattern,
		}
		var core types.ServerProductResolution
		canReuse := false
		if old != nil {
			reusable, err := coreReusable(ctx, d, old, opts.Offline)
			if err != nil {
				return err
			}
			canReuse = reusable
		}
		if canReuse {
			log.Debug(
				"reusing locked core resolution",
				"reason", "manifest core still matches lock",
				"minecraft", old.Server.Minecraft,
				"product", old.Server.Product,
				"core-version", old.Server.CoreVersion,
			)
			core = types.ServerProductResolution{
				Minecraft:      old.Server.Minecraft,
				Product:        old.Server.Product,
				ProductVersion: old.Server.CoreVersion,
				Artifact:       old.Server.Artifact,
				Bootstrapper:   old.Server.Bootstrapper,
				Inputs:         old.Server.Inputs,
				Java:           old.Server.Java,
			}
		} else {
			var err error
			core, err = routing.ResolveServerProduct(ctx, coreReq, store.materialize)
			if err != nil {
				return err
			}
		}
		resolved = &lockfile.Document{
			SchemaVersion:       lockfile.SchemaVersion,
			ManifestFingerprint: fingerprint,
			Server: lockfile.Server{
				Minecraft:    core.Minecraft,
				Directory:    d.Server.Directory,
				Product:      core.Product,
				CoreVersion:  core.ProductVersion,
				Artifact:     core.Artifact,
				Bootstrapper: core.Bootstrapper,
				Inputs:       core.Inputs,
				Java:         core.Java,
			},
			Packages: []types.LockedPackage{},
		}
		mcdrVersion := ""
		if d.MCDR != nil {
			request := d.MCDR.Version
			if old != nil && old.MCDR != nil && isFloating(request) {
				request = old.MCDR.Version
			}
			environment, err := prepareMCDR(ctx, root, d.MCDR.Directory, request, nil, store)
			if err != nil {
				return err
			}
			resolved.MCDR = environment
			mcdrVersion = string(environment.Version)
		}
		packages, err := resolve.Packages(ctx, d, old, resolved.Server, mcdrVersion, store.materialize, opts.WithOptional)
		if err != nil {
			return err
		}
		resolved.Packages = packages
		if opts.WithOptional {
			for _, p := range packages {
				for _, request := range p.Requirements {
					if p.Environment == types.EnvironmentMCDR {
						d.MCDR.Packages[request.Reference] = manifest.Requirement{
							Selector:    request.Selector,
							FilePattern: request.FilePattern,
							Enabled:     new(request.Enabled),
						}
					} else {
						if d.Server.Packages[p.Ecosystem] == nil {
							d.Server.Packages[p.Ecosystem] = map[string]manifest.Requirement{}
						}
						d.Server.Packages[p.Ecosystem][request.Reference] = manifest.Requirement{
							Selector:    request.Selector,
							FilePattern: request.FilePattern,
							Enabled:     new(request.Enabled),
						}
					}
				}
			}
			resolved.ManifestFingerprint, err = manifest.Fingerprint(d)
			if err != nil {
				return err
			}
			opts.PublishManifest = true
		}
		if d.MCDR != nil {
			var requirements []string
			for _, p := range packages {
				for _, m := range p.Modules {
					for _, dep := range m.Dependencies {
						if dep.Kind == types.DependencyKindPython {
							requirements = append(requirements, dep.ID)
						}
					}
				}
			}
			environment, err := prepareMCDR(ctx, root, d.MCDR.Directory, resolved.MCDR.Version, requirements, store)
			if err != nil {
				return err
			}
			resolved.MCDR = environment
		}
	}
	if err := resolved.Validate(); err != nil {
		return err
	}
	return stageInstall(ctx, root, d, resolved, store, opts)
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
) error {
	current, err := installationCurrent(root, resolved)
	if err != nil {
		return err
	}
	if current && !opts.PublishManifest {
		log.Info(
			"installation already current",
			"root", root,
			"packages", len(resolved.Packages),
		)
		return validateUnmanagedArtifacts(ctx, root, resolved)
	}
	reason := "installation missing or stale"
	if current {
		reason = "manifest publication forces re-staging"
	}
	log.Debug("staging installation", "root", root, "reason", reason)
	// Every path past here installs or replaces files, so the server the lock
	// describes must be runnable. A no-op replay never runs the server, so
	// demanding a local JVM earlier would fail installs that change nothing.
	if err := checkJava(ctx, resolved.Server.Java); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(filepath.Join(root, ".lucy"), "stage-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	core, err := store.materialize(&resolved.Server.Artifact)
	if err != nil {
		return err
	}
	inputs := map[string]string{}
	for i := range resolved.Server.Inputs {
		input := &resolved.Server.Inputs[i]
		if input.Embedded != "" {
			continue
		}
		p, err := store.materialize(&input.Artifact)
		if err != nil {
			return err
		}
		inputs[input.Path] = p
	}
	if err := bootstrap.PrepareLocked(ctx, stage, resolved.Server, core, inputs); err != nil {
		return err
	}
	log.Debug(
		"server tree prepared",
		"stage", stage,
		"core", resolved.Server.Filename,
		"inputs", len(inputs),
	)
	if err := rejectSharedDestinations(resolved); err != nil {
		return err
	}
	staged := 0
	for i := range resolved.Packages {
		p := &resolved.Packages[i]
		if !packageEnabled(p) {
			continue
		}
		destination, err := packageDestination(resolved, p)
		if err != nil {
			return err
		}
		source, err := store.materialize(&p.Artifact)
		if err != nil {
			return err
		}
		if err := bootstrap.CopyLockedFile(source, filepath.Join(stage, destination), 0o644); err != nil {
			return err
		}
		staged++
	}
	log.Debug("packages staged", "active", staged, "recorded", len(resolved.Packages))
	if resolved.MCDR != nil {
		if err := preparePython(ctx, root, stage, resolved, store); err != nil {
			return err
		}
	}
	if err := validateUnmanagedArtifacts(ctx, root, resolved); err != nil {
		return err
	}
	return applyCommit(root, stage, d, resolved, opts.PublishManifest)
}

func isFloating(v types.Version) bool {
	switch v {
	case "", types.VersionStable, types.VersionBeta, types.VersionAny:
		return true
	}
	return v == "latest"
}

// coreReusable reports whether a lock's server record still answers the
// manifest's core request. Resolution reuses the pin when it does; a locked
// mutation treats a negative answer as a stale lock, because rebinding
// requirements must never re-target the server.
func coreReusable(ctx context.Context, d *manifest.Document, old *lockfile.Document, offline bool) (bool, error) {
	request := types.ServerProductRequest{
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
	if request.Repository == "" && old.Server.Provider == types.ProviderGitHub {
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
		if old.Server.Provider != types.ProviderGitHub || old.Server.ProjectID != strconv.FormatInt(repository.ID, 10) {
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
		if !packageEnabled(p) {
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
func packageFolder(environment types.Environment, ecosystem types.Ecosystem) string {
	if environment == types.EnvironmentMCDR {
		return "plugins"
	}
	switch ecosystem {
	case types.EcosystemBukkit, types.EcosystemPaper:
		return "plugins"
	default:
		return "mods"
	}
}

// packageDestination reports where an enabled package lands inside the staged
// server tree.
func packageDestination(resolved *lockfile.Document, p *types.LockedPackage) (string, error) {
	directory := resolved.Server.Directory
	folder := packageFolder(p.Environment, p.Ecosystem)
	if p.Environment == types.EnvironmentMCDR {
		if resolved.MCDR == nil {
			return "", fmt.Errorf("MCDR package has no wrapper runtime")
		}
		directory = resolved.MCDR.Directory
	}
	return filepath.Join(directory, folder, p.Filename), nil
}

func packageEnabled(p *types.LockedPackage) bool {
	if len(p.Requirements) == 0 {
		return true
	}
	for _, r := range p.Requirements {
		if r.Enabled {
			return true
		}
	}
	return false
}
