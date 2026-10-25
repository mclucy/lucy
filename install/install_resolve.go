package install

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"time"

	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream/routing"
)

// Resolve builds the lock document that answers d without staging or
// committing the server tree. It records the declared packages alone, because
// the workspace the manifest describes already holds a working set that
// catalogue dependencies must not extend. Artifact bytes still enter the
// workspace download store because the lock pins their hashes. The caller must
// hold the workspace writer lock.
func Resolve(ctx context.Context, root string, d *manifest.Document, old *lockfile.Document, opts Options) (*lockfile.Document, []error, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	if d == nil {
		return nil, nil, fmt.Errorf("manifest required to resolve a lock")
	}
	opts.declaredOnly = true
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
		trace:   opts.Trace,
		fetch:   &FetchSummary{},
	}
	result := newResult()
	resolved, err := resolveInstall(ctx, absolute, d, old, fingerprint, &opts, store, result)
	if err != nil {
		return nil, result.Warnings, err
	}
	if err := resolved.Validate(); err != nil {
		return nil, result.Warnings, err
	}
	return resolved, result.Warnings, nil
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
	result *Result,
) (*lockfile.Document, error) {
	if d == nil {
		return nil, fmt.Errorf("manifest required to resolve a lock")
	}
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
	coreReq := routing.ServerProductRequest{
		Minecraft:      d.Server.Minecraft,
		Product:        d.Server.Core.Product,
		ProductVersion: d.Server.Core.ProductVersion,
		Repository:     d.Server.Core.Repository,
		FilePattern:    d.Server.Core.FilePattern,
	}
	var core routing.ServerProductResolution
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
		core = routing.ServerProductResolution{
			Minecraft:      old.Server.Minecraft,
			Product:        old.Server.Product,
			ProductVersion: old.Server.CoreVersion,
			Artifact:       old.Server.Artifact,
			Bootstrapper:   old.Server.Bootstrapper,
			Inputs:         old.Server.Inputs,
			JavaAdvisory:   old.Server.Java.Advisory,
		}
	} else {
		opts.Trace.phaseStarted("server")
		start := time.Now()
		var err error
		core, err = routing.ResolveServerProduct(ctx, coreReq, store.materialize)
		if err != nil {
			return nil, err
		}
		elapsed := time.Since(start)
		result.addPhase("server", elapsed)
		opts.Trace.phaseDone(PhaseInfo{
			Name:      "server",
			Elapsed:   elapsed,
			Product:   core.Product,
			Version:   core.ProductVersion,
			Minecraft: core.Minecraft,
		})
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
			Java:         lockfile.NewJava(core.JavaAdvisory, d.Server.Java),
		},
		Packages: []lockfile.Package{},
		Files:    slices.Clone(d.Files),
	}
	mcdrVersion := ""
	if d.MCDR != nil {
		request := d.MCDR.Version
		if old != nil && old.MCDR != nil && isFloating(request) {
			request = old.MCDR.Version
		}
		resolvedVersion, err := resolveMCDRVersion(ctx, request, old, opts.Offline)
		if err != nil {
			return nil, err
		}
		resolved.MCDR = &lockfile.MCDR{
			Directory: d.MCDR.Directory,
			Version:   resolvedVersion,
		}
		mcdrVersion = string(resolvedVersion)
	}
	opts.Trace.phaseStarted("packages")
	start := time.Now()
	var previousPackages []lockfile.Package
	if old != nil {
		previousPackages = old.Packages
	}
	packages, moduleWarnings, err := resolve.Packages(resolve.PackagesRequest{
		Context:  ctx,
		Manifest: d,
		Previous: previousPackages,
		Target: resolve.Target{
			Product:      resolved.Server.Product,
			Bootstrapper: resolved.Server.Bootstrapper,
			Minecraft:    resolved.Server.Minecraft,
			CoreVersion:  resolved.Server.CoreVersion,
			MCDRVersion:  mcdrVersion,
		},
		Materialize:        store.materialize,
		WithOptional:       opts.WithOptional,
		AcceptBroken:       opts.AcceptBroken,
		ExpandDependencies: !opts.declaredOnly,
		Trace:              opts.Trace.resolveTrace(),
	})
	if err != nil {
		return nil, err
	}
	elapsed := time.Since(start)
	result.addPhase("packages", elapsed)
	opts.Trace.phaseDone(PhaseInfo{
		Name:    "packages",
		Elapsed: elapsed,
		Count:   len(packages),
		Total:   countDeclared(d),
	})
	result.Warnings = append(result.Warnings, moduleWarnings...)
	resolved.Packages = packages
	if opts.WithOptional {
		for _, p := range packages {
			for _, request := range p.Requirements {
				if p.Environment == server.EnvironmentMCDR {
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
	return resolved, nil
}
