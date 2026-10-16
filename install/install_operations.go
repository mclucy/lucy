package install

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/workspace"
)

func commandDocuments(root string) (document *manifest.Document, locked *lockfile.Document, resultErr error) {
	writer, err := workspacewrite.Lock(root)
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := workspacewrite.RecoverInstall(root); err != nil {
		return nil, nil, err
	}
	if err := filetrack.RecoverPending(root); err != nil {
		return nil, nil, err
	}
	d, err := manifest.Read(root)
	if err != nil {
		return nil, nil, err
	}
	old, err := lockfile.Read(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	return d, old, nil
}

func selectedLoader(d *manifest.Document, requested types.Ecosystem) types.Ecosystem {
	if requested != types.EcoUnspecified {
		return requested
	}
	switch d.Server.Core.Distribution {
	case "fabric":
		return types.EcoFabric
	case "forge":
		return types.EcoForge
	case "neoforge":
		return types.EcoNeoforge
	case "paper", "purpur":
		return types.EcoBukkit
	}
	if d.Server.Core.Repository != "" {
		return types.EcoBukkit
	}
	if d.MCDR != nil {
		return types.EcoMcdr
	}
	return types.EcoUnspecified
}

func commandReference(d *manifest.Document, old *lockfile.Document, raw string, loader types.Ecosystem) (string, string, error) {
	request, err := input.Parse(raw)
	if err != nil {
		return "", "", err
	}
	provider, name := request.Source.String(), string(request.Name)
	if request.Source == types.SourceAuto {
		if old != nil {
			owner := "server"
			if loader == types.EcoMcdr {
				owner = "mcdr"
			}
			ref, err := old.ContextReference(owner, loader, name)
			if err != nil {
				return "", "", err
			}
			if ref != "" {
				provider, name, err = manifest.ParseReference(ref)
				if err != nil {
					return "", "", err
				}
			}
		}
		if provider == "auto" || provider == "" {
			provider = "modrinth"
			if loader == types.EcoMcdr {
				provider = "mcdr"
			}
		}
	}
	ref := provider + ":" + name
	if _, _, err := manifest.ParseReference(ref); err != nil {
		return "", "", err
	}
	selector := string(request.Version)
	if selector == "" || selector == "any" {
		selector = "stable"
	}
	return ref, selector, nil
}

func Add(ctx context.Context, root string, args []string, loader types.Ecosystem, opts Options) ([]filetrack.Diagnostic, error) {
	d, old, err := commandDocuments(root)
	if err != nil {
		return nil, err
	}
	opts.expectedManifest, err = manifest.Fingerprint(d)
	if err != nil {
		return nil, err
	}
	opts.requirementChange = true
	loader = selectedLoader(d, loader)
	if loader == types.EcoUnspecified {
		return nil, fmt.Errorf("specify a package loader supported by the declared server")
	}
	for _, raw := range args {
		ref, selector, err := commandReference(d, old, raw, loader)
		if err != nil {
			return nil, err
		}
		r := manifest.Requirement{Version: selector, Enabled: new(true)}
		if loader == types.EcoMcdr {
			if d.MCDR == nil {
				return nil, fmt.Errorf("manifest has no MCDR runtime")
			}
			d.MCDR.Packages[ref] = r
		} else {
			if d.Server.Packages[loader] == nil {
				d.Server.Packages[loader] = map[string]manifest.Requirement{}
			}
			d.Server.Packages[loader][ref] = r
		}
	}
	opts.PublishManifest = true
	return Sync(ctx, root, d, old, opts)
}

func Remove(ctx context.Context, root string, args []string, loader types.Ecosystem, opts Options) ([]filetrack.Diagnostic, error) {
	d, old, err := commandDocuments(root)
	if err != nil {
		return nil, err
	}
	opts.expectedManifest, err = manifest.Fingerprint(d)
	if err != nil {
		return nil, err
	}
	opts.requirementChange = true
	loader = selectedLoader(d, loader)
	for _, raw := range args {
		ref, _, err := commandReference(d, old, raw, loader)
		if err != nil {
			return nil, err
		}
		packages := d.Server.Packages[loader]
		if loader == types.EcoMcdr {
			if d.MCDR == nil {
				return nil, fmt.Errorf("manifest has no MCDR runtime")
			}
			packages = d.MCDR.Packages
		}
		if _, ok := packages[ref]; !ok {
			return nil, fmt.Errorf("direct requirement %s not found in %s", ref, loader)
		}
		delete(packages, ref)
	}
	opts.PublishManifest = true
	return Sync(ctx, root, d, old, opts)
}

func Initialize(ctx context.Context, root string, opts InitOptions) (document *manifest.Document, warnings []error, resultErr error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, nil, err
	}
	writer, err := workspacewrite.Lock(root)
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := workspacewrite.RecoverInstall(root); err != nil {
		return nil, nil, err
	}
	if err := filetrack.RecoverPending(root); err != nil {
		return nil, nil, err
	}
	if _, err := os.Stat(filepath.Join(root, manifest.Filename)); err == nil && !opts.Force {
		return nil, nil, fmt.Errorf("lucy.yaml exists; use --force to replace declared intent")
	}
	d := &manifest.Document{Version: manifest.FormatVersion, Server: manifest.Server{
		Minecraft: opts.Minecraft, Directory: opts.Directory,
		Core:     manifest.Core{Distribution: opts.Distribution, Version: opts.CoreVersion, Repository: opts.Repository},
		Packages: map[types.Ecosystem]map[string]manifest.Requirement{},
	}}
	var observed workspace.Workspace
	if opts.Detect {
		observed = workspace.NewAt(root)
		if observed.Probe.HasAmbiguity() {
			return nil, observed.Warnings, fmt.Errorf(
				"cannot initialize an ambiguous server runtime: %w",
				errors.Join(observed.Probe.AmbiguityErrors(observed.Root)...),
			)
		}
		if server := observed.Server(); server != nil {
			if d.Server.Minecraft != "" && d.Server.Minecraft != string(server.GameVersion()) {
				return nil, observed.Warnings, fmt.Errorf("detected Minecraft %s, requested %s", server.GameVersion(), d.Server.Minecraft)
			}
			d.Server.Minecraft = string(server.GameVersion())
			if d.Server.Core.Distribution == "" {
				d.Server.Core.Distribution = server.ServerCore()
				if d.Server.Core.Distribution == "minecraft" {
					d.Server.Core.Distribution = "vanilla"
				}
			}
			if d.Server.Core.Version == "" && server.PrimaryRuntime.Version != "" && !server.PrimaryRuntime.Version.IsInvalid() && !server.PrimaryRuntime.Version.CanInfer() {
				d.Server.Core.Version = string(server.PrimaryRuntime.Version)
			}
			if d.Server.Directory == "" {
				relative, err := filepath.Rel(root, observed.Root)
				if err != nil {
					return nil, observed.Warnings, err
				}
				d.Server.Directory = filepath.ToSlash(relative)
			}
			log.Debug(
				"detected server runtime",
				"minecraft", d.Server.Minecraft,
				"distribution", d.Server.Core.Distribution,
				"core-version", d.Server.Core.Version,
				"directory", d.Server.Directory,
			)
		} else if !opts.AllowEmpty {
			return nil, observed.Warnings, fmt.Errorf("no server detected; use create or --allow-empty with a complete server declaration")
		} else {
			log.Debug(
				"no server detected; building a complete declaration",
				"reason", "--allow-empty",
			)
		}
	}
	if d.Server.Minecraft == "" || types.BareVersion(d.Server.Minecraft).IsInvalid() {
		if opts.PromptMinecraft == nil {
			return nil, observed.Warnings, fmt.Errorf("a concrete --minecraft version is required")
		}
		text, err := opts.PromptMinecraft()
		if err != nil {
			return nil, observed.Warnings, err
		}
		d.Server.Minecraft = text
		log.Debug("minecraft version supplied interactively", "version", text)
	}
	if d.Server.Core.Distribution == "" {
		d.Server.Core.Distribution = "vanilla"
		log.Debug("core distribution defaulted", "distribution", "vanilla")
	}
	mcdrVersion := opts.MCDRVersion
	if mcdrVersion == "" && observed.Environments.Mcdr != nil {
		v := observed.Environments.Mcdr.Version
		if v.IsInvalid() || v.CanInfer() {
			return nil, observed.Warnings, fmt.Errorf("MCDR detected without an exact app version; specify --mcdr-version")
		}
		mcdrVersion = string(v)
		log.Debug("MCDR version from workspace detection", "version", mcdrVersion)
	}
	if mcdrVersion != "" {
		d.MCDR = &manifest.MCDR{Directory: ".", Version: mcdrVersion, Packages: map[string]manifest.Requirement{}}
	}
	if err := manifest.Normalize(d); err != nil {
		return nil, observed.Warnings, err
	}
	modrinthMapper, hasModrinthMapper, err := routing.GetArtifactMapper(types.SourceModrinth)
	if err != nil {
		return nil, observed.Warnings, err
	}
	recognized := 0
	for _, p := range observed.Packages {
		if !artifact.SupportsPath(p.Path) {
			continue
		}
		loader := p.Id.Eco
		if loader == types.EcoPaper {
			loader = types.EcoBukkit
		}
		modules, err := artifact.Inspect(ctx, p.Path, loader)
		if err != nil || len(modules) == 0 {
			continue
		}
		if loader == types.EcoMcdr && d.MCDR != nil {
			candidates, err := routing.Candidates(ctx, "mcdr", modules[0].ID, d.Server.Minecraft, loader)
			if err != nil {
				continue
			}
			for _, candidate := range candidates {
				if candidate.Hashes["sha256"] == "" {
					continue
				}
				if _, err := digestFile(p.Path, candidate.Hashes); err == nil {
					d.MCDR.Packages["mcdr:"+candidate.ProjectID] = manifest.Requirement{Version: candidate.Version}
					recognized++
					break
				}
			}
			continue
		}
		if loader != types.EcoFabric && loader != types.EcoForge && loader != types.EcoNeoforge && loader != types.EcoBukkit {
			continue
		}
		if !hasModrinthMapper {
			continue
		}
		ref, _, matched, err := modrinthMapper.PackageByHash(artifact.File{Path: p.Path})
		if err != nil || !matched || ref.Name == "" {
			continue
		}
		if d.Server.Packages[loader] == nil {
			d.Server.Packages[loader] = map[string]manifest.Requirement{}
		}
		d.Server.Packages[loader]["modrinth:"+ref.Name.String()] = manifest.Requirement{Version: ref.Version.String()}
		recognized++
	}
	log.Debug(
		"recognized existing packages",
		"count", recognized,
		"present", len(observed.Packages),
	)
	store := &artifactStore{ctx: ctx, dir: filepath.Join(root, ".lucy", "downloads")}
	if _, err := routing.ResolveCore(ctx, types.CoreRequest{Minecraft: d.Server.Minecraft, Distribution: d.Server.Core.Distribution, Version: d.Server.Core.Version, Repository: d.Server.Core.Repository, File: d.Server.Core.File}, store.acquire); err != nil {
		return nil, observed.Warnings, err
	}
	if err := manifest.Write(root, d); err != nil {
		return nil, observed.Warnings, err
	}
	return d, observed.Warnings, nil
}
