package install

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
)

// Resolve builds the lock document that answers d without staging or
// committing the server tree. Artifact bytes still enter the workspace
// download store because the lock pins their hashes. The caller must hold the
// workspace writer lock.
func Resolve(ctx context.Context, root string, d *manifest.Document, old *lockfile.Document, opts Options) (*lockfile.Document, []error, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	if d == nil {
		return nil, nil, fmt.Errorf("manifest required to resolve a lock")
	}
	if err := workspacewrite.SafePath(absolute, ".lucy"); err != nil {
		return nil, nil, err
	}
	fingerprint, err := manifest.Fingerprint(d)
	if err != nil {
		return nil, nil, err
	}
	if old != nil && !opts.WithOptional && old.ManifestFingerprint == fingerprint {
		log.Debug("lock resolution reused current lock", "reason", "manifest unchanged")
		return old, nil, nil
	}
	store := &downloadStore{
		ctx:     ctx,
		dir:     filepath.Join(absolute, ".lucy", "downloads"),
		offline: opts.Offline,
	}
	var warnings []error
	resolved, err := resolveInstall(ctx, absolute, d, old, fingerprint, &opts, store, &warnings)
	if err != nil {
		return nil, warnings, err
	}
	if err := resolved.Validate(); err != nil {
		return nil, warnings, err
	}
	return resolved, warnings, nil
}

// resolveInstall resolves d into a new lock document. The caller decides when
// a lock may be reused, so this function always resolves. The download store
// receives every artifact whose hash the lock records.
func resolveInstall(
	ctx context.Context,
	root string,
	d *manifest.Document,
	old *lockfile.Document,
	fingerprint string,
	opts *Options,
	store *downloadStore,
	warnings *[]error,
) (*lockfile.Document, error) {
	if opts.Offline {
		return nil, fmt.Errorf("offline resolution requires a current lock; metadata selection is unavailable")
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
			return nil, err
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
			return nil, err
		}
	}
	resolved := &lockfile.Document{
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
		Files:    slices.Clone(d.Files),
	}
	mcdrVersion := ""
	if d.MCDR != nil {
		request := d.MCDR.Version
		if old != nil && old.MCDR != nil && isFloating(request) {
			request = old.MCDR.Version
		}
		environment, err := prepareMCDR(ctx, root, d.MCDR.Directory, request, nil, store)
		if err != nil {
			return nil, err
		}
		resolved.MCDR = environment
		mcdrVersion = string(environment.Version)
	}
	packages, moduleWarnings, err := resolve.Packages(ctx, d, old, resolved.Server, mcdrVersion, store.materialize, opts.WithOptional, opts.AcceptBroken)
	if err != nil {
		return nil, err
	}
	*warnings = append(*warnings, moduleWarnings...)
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
			return nil, err
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
			return nil, err
		}
		resolved.MCDR = environment
	}
	return resolved, nil
}
