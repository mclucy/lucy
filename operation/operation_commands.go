package operation

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/resolve"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/workspace"
)

// InitOptions describes a new manifest declaration.
type InitOptions struct {
	Minecraft, Product, ProductVersion, Repository, MCDRVersion, Directory string
	Force, AllowEmpty, Detect                                              bool
	// PromptMinecraft asks for a concrete Minecraft version when
	// initialization lacks one and returns the trimmed response. Nil keeps
	// the concrete-version-required error and never prompts.
	PromptMinecraft func() (string, error)
}

func commandDocuments(root string) (document *manifest.Document, locked *lockfile.Document, resultErr error) {
	writer, err := workspacewrite.Lock(root)
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := workspacewrite.RecoverInstall(root); err != nil {
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

func selectedEcosystem(d *manifest.Document, requested types.Ecosystem) types.Ecosystem {
	if requested != types.EcosystemUnspecified {
		return requested
	}
	switch d.Server.Core.Product {
	case types.ServerProductFabric:
		return types.EcosystemFabric
	case types.ServerProductForge:
		return types.EcosystemForge
	case types.ServerProductNeoForge:
		return types.EcosystemNeoForge
	case types.ServerProductPaper, types.ServerProductPurpur:
		return types.EcosystemPaper
	}
	if d.Server.Core.Repository != "" {
		return types.EcosystemPaper
	}
	if d.MCDR != nil {
		return types.EcosystemMCDR
	}
	return types.EcosystemUnspecified
}

// servedEcosystemSet returns the package ecosystems that the declared server
// can load. A probed runtime also reports bridged ecosystems, such as Fabric
// through Sinytra Connector. Only ecosystems the declared product loads enter
// the set. Without a probe, the declared product decides.
func servedEcosystemSet(d *manifest.Document, observed workspace.Workspace) map[types.Ecosystem]struct{} {
	server := lockfile.Server{Product: d.Server.Core.Product}
	served := map[types.Ecosystem]struct{}{}
	for _, offer := range observed.ServedEcosystems() {
		if resolve.SupportsPackages(server, offer.Ecosystem) {
			served[offer.Ecosystem] = struct{}{}
		}
	}
	if len(served) == 0 {
		if ecosystem := selectedEcosystem(d, types.EcosystemUnspecified); ecosystem != types.EcosystemUnspecified {
			served[ecosystem] = struct{}{}
		}
	}
	return served
}

// servedEcosystemForFile reports the served loader whose descriptor the file
// carries. A jar without a top-level descriptor can declare its loader inside
// a JarJar container, while the upstream project label may name a loader the
// server does not offer.
func servedEcosystemForFile(
	ctx context.Context,
	path string,
	served map[types.Ecosystem]struct{},
) (types.Ecosystem, bool) {
	for _, ecosystem := range slices.Sorted(maps.Keys(served)) {
		if ecosystem == types.EcosystemMCDR {
			continue
		}
		modules, err := artifact.ReadModules(ctx, path, ecosystem)
		if err != nil {
			continue
		}
		for _, module := range modules {
			if loaderSpecificDescriptor(module) {
				return ecosystem, true
			}
		}
	}
	return types.EcosystemUnspecified, false
}

// loaderSpecificDescriptor reports whether a module's descriptor names the
// loader that read it. Forge and NeoForge share META-INF/mods.toml, so only a
// loader dependency declared inside it identifies the loader.
func loaderSpecificDescriptor(module types.DescriptorModule) bool {
	if module.DescriptorPath != "META-INF/mods.toml" {
		return true
	}
	loader := module.Ecosystem.String()
	return slices.ContainsFunc(module.Dependencies, func(dep types.DescriptorDependency) bool {
		return dep.ID == loader
	})
}

func commandReference(d *manifest.Document, old *lockfile.Document, raw string, ecosystem types.Ecosystem) (string, types.Version, error) {
	request, err := input.Parse(raw)
	if err != nil {
		return "", "", err
	}
	provider, name := request.Provider, string(request.Name)
	if provider == types.ProviderUnspecified {
		if old != nil {
			ref, err := old.LookupReference(name)
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
		if provider == types.ProviderUnspecified {
			provider = types.ProviderModrinth
			if ecosystem == types.EcosystemMCDR {
				provider = types.ProviderMCDR
			}
		}
	}
	ref := provider.String() + ":" + name
	if _, _, err := manifest.ParseReference(ref); err != nil {
		return "", "", err
	}
	selector := request.Version
	if selector == "" {
		selector = types.VersionAny
	}
	return ref, selector, nil
}

func Add(ctx context.Context, root string, args []string, ecosystem types.Ecosystem, opts install.Options) ([]filetrack.Diagnostic, []error, error) {
	d, old, err := commandDocuments(root)
	if err != nil {
		return nil, nil, err
	}
	opts.ExpectedManifest, err = manifest.Fingerprint(d)
	if err != nil {
		return nil, nil, err
	}
	opts.RequirementChange = true
	ecosystem = selectedEcosystem(d, ecosystem)
	if ecosystem == types.EcosystemUnspecified {
		return nil, nil, fmt.Errorf("specify a package ecosystem supported by the declared server")
	}
	for _, raw := range args {
		ref, selector, err := commandReference(d, old, raw, ecosystem)
		if err != nil {
			return nil, nil, err
		}
		r := manifest.Requirement{Selector: selector, Enabled: new(true)}
		if ecosystem == types.EcosystemMCDR {
			if d.MCDR == nil {
				return nil, nil, fmt.Errorf("manifest has no MCDR runtime")
			}
			d.MCDR.Packages[ref] = r
		} else {
			if d.Server.Packages[ecosystem] == nil {
				d.Server.Packages[ecosystem] = map[string]manifest.Requirement{}
			}
			d.Server.Packages[ecosystem][ref] = r
		}
	}
	opts.PublishManifest = true
	return install.Sync(ctx, root, d, old, opts)
}

func Remove(ctx context.Context, root string, args []string, ecosystem types.Ecosystem, opts install.Options) ([]filetrack.Diagnostic, []error, error) {
	d, old, err := commandDocuments(root)
	if err != nil {
		return nil, nil, err
	}
	opts.ExpectedManifest, err = manifest.Fingerprint(d)
	if err != nil {
		return nil, nil, err
	}
	opts.RequirementChange = true
	ecosystem = selectedEcosystem(d, ecosystem)
	for _, raw := range args {
		ref, _, err := commandReference(d, old, raw, ecosystem)
		if err != nil {
			return nil, nil, err
		}
		packages := d.Server.Packages[ecosystem]
		if ecosystem == types.EcosystemMCDR {
			if d.MCDR == nil {
				return nil, nil, fmt.Errorf("manifest has no MCDR runtime")
			}
			packages = d.MCDR.Packages
		}
		if _, ok := packages[ref]; !ok {
			return nil, nil, fmt.Errorf("direct requirement %s not found in %s", ref, ecosystem)
		}
		delete(packages, ref)
	}
	opts.PublishManifest = true
	return install.Sync(ctx, root, d, old, opts)
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
	product := types.ServerProductUnknown
	if opts.Product != "" {
		if parsed, known := types.ParseServerProduct(opts.Product); known {
			product = parsed
		} else {
			product = types.ServerProduct(opts.Product)
		}
	}
	d := &manifest.Document{SchemaVersion: manifest.SchemaVersion, Server: manifest.Server{
		Minecraft: types.Version(opts.Minecraft), Directory: opts.Directory,
		Core:     manifest.Core{Product: product, ProductVersion: types.Version(opts.ProductVersion), Repository: opts.Repository},
		Packages: map[types.Ecosystem]map[string]manifest.Requirement{},
	}}
	var observed workspace.Workspace
	if opts.Detect {
		observed = workspace.NewAt(root)
		if observed.Probe.HasAmbiguity() {
			return nil, observed.Warnings, fmt.Errorf(
				"cannot initialize an ambiguous server runtime: %w",
				errors.Join(observed.Probe.AmbiguityErrors(observed.ServerDir)...),
			)
		}
		if server := observed.Runtime(); server != nil {
			if d.Server.Minecraft != "" && d.Server.Minecraft != server.MinecraftVersion() {
				return nil, observed.Warnings, fmt.Errorf("detected Minecraft %s, requested %s", server.MinecraftVersion(), d.Server.Minecraft)
			}
			d.Server.Minecraft = server.MinecraftVersion()
			if d.Server.Core.Product == types.ServerProductUnknown {
				if product := server.Product(); product != types.ServerProductUnknown {
					d.Server.Core.Product = product
				} else if server.Primary != nil {
					d.Server.Core.Product = types.ServerProduct(server.Primary.Name.String())
				}
			}
			if d.Server.Core.ProductVersion == "" && server.Primary.Version != "" && !server.Primary.Version.IsInvalid() && !server.Primary.Version.CanInfer() {
				d.Server.Core.ProductVersion = server.Primary.Version
			}
			if d.Server.Directory == "" {
				relative, err := filepath.Rel(root, observed.ServerDir)
				if err != nil {
					return nil, observed.Warnings, err
				}
				d.Server.Directory = filepath.ToSlash(relative)
			}
			log.Debug(
				"detected server runtime",
				"minecraft", d.Server.Minecraft,
				"product", d.Server.Core.Product,
				"core-version", d.Server.Core.ProductVersion,
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
	if d.Server.Minecraft == "" || d.Server.Minecraft.IsInvalid() {
		if opts.PromptMinecraft == nil {
			return nil, observed.Warnings, fmt.Errorf("a concrete --minecraft version is required")
		}
		text, err := opts.PromptMinecraft()
		if err != nil {
			return nil, observed.Warnings, err
		}
		d.Server.Minecraft = types.Version(text)
		log.Debug("minecraft version supplied interactively", "version", text)
	}
	if d.Server.Core.Product == types.ServerProductUnknown {
		d.Server.Core.Product = types.ServerProductMinecraft
		log.Debug("core product defaulted", "product", d.Server.Core.Product)
	}
	mcdrVersion := opts.MCDRVersion
	if mcdrVersion == "" && observed.Wrappers.MCDR != nil {
		v := observed.Wrappers.MCDR.Version
		if v.IsInvalid() || v.CanInfer() {
			return nil, observed.Warnings, fmt.Errorf("MCDR detected without an exact app version; specify --mcdr-version")
		}
		mcdrVersion = string(v)
		log.Debug("MCDR version from workspace detection", "version", mcdrVersion)
	}
	if mcdrVersion != "" {
		d.MCDR = &manifest.MCDR{Directory: ".", Version: types.Version(mcdrVersion), Packages: map[string]manifest.Requirement{}}
	}
	if err := manifest.Normalize(d); err != nil {
		return nil, observed.Warnings, err
	}
	modrinthMapper, hasModrinthMapper, err := routing.GetArtifactMapper(types.ProviderModrinth)
	if err != nil {
		return nil, observed.Warnings, err
	}
	served := servedEcosystemSet(d, observed)
	recognized := 0
	for _, p := range observed.Packages {
		if !artifact.SupportsPath(p.Path) {
			continue
		}
		ecosystem := p.ID.Ecosystem
		if ecosystem != types.EcosystemMCDR {
			if _, ok := served[ecosystem]; !ok {
				var adopted bool
				ecosystem, adopted = servedEcosystemForFile(ctx, p.Path, served)
				if !adopted {
					continue
				}
			}
		}
		modules, err := artifact.ReadModules(ctx, p.Path, ecosystem)
		if err != nil || len(modules) == 0 {
			continue
		}
		if ecosystem == types.EcosystemMCDR && d.MCDR != nil {
			candidates, err := routing.Candidates(ctx, types.RegistryMCDR, modules[0].ID, string(d.Server.Minecraft), ecosystem)
			if err != nil {
				continue
			}
			for _, candidate := range candidates {
				if candidate.Hashes["sha256"] == "" {
					continue
				}
				if _, err := install.DigestFile(p.Path, candidate.Hashes); err == nil {
					d.MCDR.Packages["mcdr:"+candidate.ProjectID] = manifest.Requirement{Selector: candidate.Version}
					recognized++
					break
				}
			}
			continue
		}
		if ecosystem != types.EcosystemFabric && ecosystem != types.EcosystemForge && ecosystem != types.EcosystemNeoForge && ecosystem != types.EcosystemBukkit && ecosystem != types.EcosystemPaper {
			continue
		}
		if !hasModrinthMapper {
			continue
		}
		ref, _, matched, err := modrinthMapper.PackageByHash(artifact.File{Path: p.Path})
		if err != nil || !matched || ref.Name == "" {
			continue
		}
		// A manifest entry that resolution cannot select blocks every later
		// command, so an installed package whose catalogue metadata offers no
		// matching release for this server stays unmanaged.
		candidates, candidateErr := routing.Candidates(
			ctx, types.RegistryModrinth, ref.Name.String(),
			string(d.Server.Minecraft), ecosystem,
		)
		usable := candidateErr == nil && slices.ContainsFunc(
			candidates,
			func(candidate types.CatalogueCandidate) bool {
				return candidate.Version == ref.Version && candidate.PrimaryFile
			},
		)
		if !usable {
			if candidateErr == nil {
				candidateErr = fmt.Errorf("no release provides version %s", ref.Version)
			}
			observed.Warnings = append(observed.Warnings, fmt.Errorf("package %s left unmanaged: %w", ref.Name, candidateErr))
			continue
		}
		if d.Server.Packages[ecosystem] == nil {
			d.Server.Packages[ecosystem] = map[string]manifest.Requirement{}
		}
		d.Server.Packages[ecosystem]["modrinth:"+ref.Name.String()] = manifest.Requirement{Selector: ref.Version}
		recognized++
	}
	log.Debug(
		"recognized existing packages",
		"count", recognized,
		"present", len(observed.Packages),
	)
	old, err := lockfile.Read(root)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, observed.Warnings, err
	}
	if errors.Is(err, os.ErrNotExist) {
		old = nil
	}
	// Adoption records the set the server already runs, so module
	// requirements that the installed jars report but the catalogue metadata
	// cannot satisfy become warnings instead of a refusal.
	resolved, resolveWarnings, err := install.Resolve(ctx, root, d, old, install.Options{AcceptBroken: true})
	observed.Warnings = append(observed.Warnings, resolveWarnings...)
	if err != nil {
		return nil, observed.Warnings, err
	}
	if err := manifest.Write(root, d); err != nil {
		return nil, observed.Warnings, err
	}
	if resolved != old {
		if err := lockfile.Write(root, resolved); err != nil {
			return nil, observed.Warnings, err
		}
	}
	return d, observed.Warnings, nil
}
