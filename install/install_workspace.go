package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/workspace"
)

type (
	Options struct {
		Locked           bool
		Offline          bool
		PublishManifest  bool
		WithOptional     bool
		expectedManifest string
		// requirementChange records that the caller edited declared package
		// requirements, so a lock whose fingerprint no longer matches is
		// expected rather than stale.
		requirementChange bool
	}
	InitOptions struct {
		Minecraft, Distribution, CoreVersion, Repository, MCDRVersion, Directory string
		Force, AllowEmpty, Detect                                                bool
	}
)

func Run(ctx context.Context, root string, opts Options) error {
	d, err := manifest.Read(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		d = nil
	}
	old, err := lockfile.Read(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if errors.Is(err, os.ErrNotExist) {
		old = nil
	}
	return Sync(ctx, root, d, old, opts)
}

func Sync(ctx context.Context, root string, d *manifest.Document, old *lockfile.Document, opts Options) error {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	root = absolute
	if err := safeOutputPath(root, ".lucy"); err != nil {
		return err
	}
	if d == nil && old == nil {
		return fmt.Errorf("lucy.yaml or lucy-lock.yaml is required")
	}
	if err := os.MkdirAll(filepath.Join(root, ".lucy"), 0o755); err != nil {
		return err
	}
	writer, err := lockWorkspace(root)
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
	if workspace.ActiveAt(filepath.Join(root, serverDirectory)) {
		return fmt.Errorf("stop the running Minecraft server before installation")
	}
	if err := recoverCommit(root); err != nil {
		return err
	}
	if opts.expectedManifest != "" {
		current, err := manifest.Read(root)
		if err != nil {
			return err
		}
		fingerprint, err := manifest.Fingerprint(current)
		if err != nil {
			return err
		}
		if fingerprint != opts.expectedManifest {
			return fmt.Errorf("manifest changed during package selection; retry from current intent")
		}
	}
	store := &artifactStore{ctx: ctx, dir: filepath.Join(root, ".lucy", "downloads"), offline: opts.Offline}
	resolved := old
	fingerprint := ""
	if d != nil {
		var err error
		fingerprint, err = manifest.Fingerprint(d)
		if err != nil {
			return err
		}
	}
	if old == nil || opts.WithOptional || (d != nil && old.ManifestFingerprint != fingerprint) {
		if opts.Locked {
			if !opts.requirementChange || old == nil || d == nil {
				return fmt.Errorf("locked installation requires a current lucy-lock.yaml")
			}
			reusable, err := coreReusable(ctx, d, old, opts.Offline)
			if err != nil {
				return err
			}
			if !reusable {
				return fmt.Errorf("manifest core no longer matches lucy-lock.yaml; install without --locked")
			}
			resolved, err = mutateLock(d, old, fingerprint)
			if err != nil {
				return err
			}
			if err := lockfile.Validate(resolved); err != nil {
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
		coreReq := types.CoreRequest{Minecraft: d.Server.Minecraft, Distribution: d.Server.Core.Distribution, Version: d.Server.Core.Version, Repository: d.Server.Core.Repository, File: d.Server.Core.File}
		var core types.CoreResolution
		canReuse := false
		if old != nil {
			reusable, err := coreReusable(ctx, d, old, opts.Offline)
			if err != nil {
				return err
			}
			canReuse = reusable
		}
		if canReuse {
			core = types.CoreResolution{Minecraft: old.Server.Minecraft, Distribution: old.Server.Distribution, Version: old.Server.CoreVersion, Artifact: old.Server.Artifact, Bootstrap: old.Server.Bootstrap, Inputs: old.Server.Inputs, Java: old.Server.Java}
		} else {
			var err error
			core, err = routing.ResolveCore(ctx, coreReq, store.acquire)
			if err != nil {
				return err
			}
		}
		resolved = &lockfile.Document{Version: lockfile.FormatVersion, ManifestFingerprint: fingerprint, Server: lockfile.Server{Minecraft: core.Minecraft, Directory: d.Server.Directory, Distribution: core.Distribution, CoreVersion: core.Version, Artifact: core.Artifact, Bootstrap: core.Bootstrap, Inputs: core.Inputs, Java: core.Java}, Packages: []types.LockedPackage{}}
		mcdrVersion := ""
		if d.MCDR != nil {
			request := d.MCDR.Version
			if old != nil && old.MCDR != nil && isFloating(request) {
				request = old.MCDR.Version
			}
			environment, err := resolvePython(ctx, root, d.MCDR.Directory, request, nil, store)
			if err != nil {
				return err
			}
			resolved.MCDR = environment
			mcdrVersion = environment.Version
		}
		packages, err := resolve.Packages(ctx, d, old, resolved.Server, mcdrVersion, store.acquire, opts.WithOptional)
		if err != nil {
			return err
		}
		resolved.Packages = packages
		if opts.WithOptional {
			for _, p := range packages {
				for _, request := range p.Requirements {
					if p.Runtime == "mcdr" {
						d.MCDR.Packages[request.Reference] = manifest.Requirement{Version: request.Selector, File: request.File, Enabled: new(request.Enabled)}
					} else {
						if d.Server.Packages[p.Loader] == nil {
							d.Server.Packages[p.Loader] = map[string]manifest.Requirement{}
						}
						d.Server.Packages[p.Loader][request.Reference] = manifest.Requirement{Version: request.Selector, File: request.File, Enabled: new(request.Enabled)}
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
			requirements := []string{}
			for _, p := range packages {
				for _, m := range p.Modules {
					for _, dep := range m.Dependencies {
						if dep.Kind == "python" {
							requirements = append(requirements, dep.ID)
						}
					}
				}
			}
			environment, err := resolvePython(ctx, root, d.MCDR.Directory, resolved.MCDR.Version, requirements, store)
			if err != nil {
				return err
			}
			resolved.MCDR = environment
		}
	}
	if err := lockfile.Validate(resolved); err != nil {
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
	store *artifactStore,
	opts Options,
) error {
	if err := checkJava(ctx, resolved.Server.Java); err != nil {
		return err
	}
	current, err := installationCurrent(root, resolved)
	if err != nil {
		return err
	}
	if current && !opts.PublishManifest {
		return validateAmbient(ctx, root, resolved)
	}
	stage, err := os.MkdirTemp(filepath.Join(root, ".lucy"), "stage-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	core, err := store.acquire(&resolved.Server.Artifact)
	if err != nil {
		return err
	}
	inputs := map[string]string{}
	for i := range resolved.Server.Inputs {
		input := &resolved.Server.Inputs[i]
		if input.Embedded != "" {
			continue
		}
		p, err := store.acquire(&input.Artifact)
		if err != nil {
			return err
		}
		inputs[input.Path] = p
	}
	if err := bootstrap.PrepareLocked(ctx, stage, resolved.Server, core, inputs); err != nil {
		return err
	}
	for i := range resolved.Packages {
		p := &resolved.Packages[i]
		active := len(p.Requirements) == 0
		for _, r := range p.Requirements {
			active = active || r.Enabled
		}
		if !active {
			continue
		}
		source, err := store.acquire(&p.Artifact)
		if err != nil {
			return err
		}
		directory := resolved.Server.Directory
		folder := "mods"
		if p.Loader == types.EcoBukkit {
			folder = "plugins"
		}
		if p.Runtime == "mcdr" {
			if resolved.MCDR == nil {
				return fmt.Errorf("MCDR package has no wrapper runtime")
			}
			directory = resolved.MCDR.Directory
			folder = "plugins"
		}
		if err := bootstrap.CopyLockedFile(source, filepath.Join(stage, directory, folder, p.Filename), 0o644); err != nil {
			return err
		}
	}
	if resolved.MCDR != nil {
		if err := preparePython(ctx, root, stage, resolved, store); err != nil {
			return err
		}
	}
	if err := validateAmbient(ctx, root, resolved); err != nil {
		return err
	}
	return applyCommit(root, stage, d, resolved, opts.PublishManifest)
}

func isFloating(v string) bool {
	return v == "" || v == "stable" || v == "beta" || v == "any" || v == "latest"
}

// coreReusable reports whether a lock's server record still answers the
// manifest's core request. Resolution reuses the pin when it does; a locked
// mutation treats a negative answer as a stale lock, because rebinding
// requirements must never re-target the server.
func coreReusable(ctx context.Context, d *manifest.Document, old *lockfile.Document, offline bool) (bool, error) {
	request := types.CoreRequest{Minecraft: d.Server.Minecraft, Distribution: d.Server.Core.Distribution, Version: d.Server.Core.Version, Repository: d.Server.Core.Repository, File: d.Server.Core.File}
	if old.Server.Directory != d.Server.Directory {
		return false, nil
	}
	if old.Server.Minecraft != request.Minecraft || old.Server.Distribution != request.Distribution {
		return false, nil
	}
	if !isFloating(request.Version) && old.Server.CoreVersion != request.Version {
		return false, nil
	}
	// Dropping the repository override retargets the server at its official
	// distribution, so a locked GitHub artifact no longer satisfies the request
	// even though every pinned field still matches.
	if request.Repository == "" && old.Server.Provider == "github" {
		return false, nil
	}
	if request.Repository != "" {
		if offline {
			return false, nil
		}
		var repository struct {
			ID int64 `json:"id"`
		}
		if err := routing.Metadata(ctx, "https://api.github.com/repos/"+request.Repository, &repository); err != nil {
			return false, err
		}
		if old.Server.Provider != "github" || old.Server.ProjectID != strconv.FormatInt(repository.ID, 10) {
			return false, nil
		}
	}
	if request.File != "" {
		matched, err := filepath.Match(request.File, old.Server.Filename)
		if err != nil {
			return false, err
		}
		if !matched {
			return false, nil
		}
	}
	return true, nil
}
