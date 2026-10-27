package operation

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"

	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/resolve"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
	"github.com/mclucy/lucy/workspace/identify"
)

// InitOptions describes adoption of the server already present in a
// directory.
type InitOptions struct {
	MCDRVersion string
	Force       bool
	// EntryPoint selects the runtime observation uses for this
	// initialization only; it is never persisted.
	EntryPoint string
	// PromptRuntime asks which entry point to use when observation finds
	// several equally valid runtimes. Nil keeps the strict failure.
	PromptRuntime func(discovery.Result) (string, error)
	// Trace receives progress for the lock resolution initialization runs.
	Trace *install.Trace
}

// InitResult is the structured outcome of one initialization. The document
// is the written manifest; warnings and discovery carry the observations
// that the command boundary presents.
type InitResult struct {
	Document      *documentpkg.Manifest
	Lock          *documentpkg.Lock
	PublishedLock bool
	Warnings      []error
	Discovery     discovery.Result
}

func commandDocuments(root string) (document *documentpkg.Manifest, locked *documentpkg.Lock, resultErr error) {
	writer, err := workspacewrite.Lock(root)
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := workspacewrite.RecoverInstall(root); err != nil {
		return nil, nil, err
	}
	d, err := documentpkg.ManifestFile(root).Read()
	if err != nil {
		return nil, nil, err
	}
	old, err := documentpkg.LockFile(root).Read()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	return d, old, nil
}

func selectedEcosystem(d *documentpkg.Manifest, requested catalog.Ecosystem) catalog.Ecosystem {
	if requested != catalog.EcosystemUnspecified {
		return requested
	}
	switch d.Server.Core.Product {
	case serverpkg.ProductFabric:
		return catalog.EcosystemFabric
	case serverpkg.ProductForge:
		return catalog.EcosystemForge
	case serverpkg.ProductNeoForge:
		return catalog.EcosystemNeoForge
	case serverpkg.ProductPaper, serverpkg.ProductPurpur:
		return catalog.EcosystemPaper
	}
	if d.Server.Core.Repository != "" {
		return catalog.EcosystemPaper
	}
	if d.MCDR != nil {
		return catalog.EcosystemMCDR
	}
	return catalog.EcosystemUnspecified
}

// servedEcosystemSet returns the package ecosystems that the declared server
// can load. A probed runtime also reports bridged ecosystems, such as Fabric
// through Sinytra Connector. Only ecosystems the declared product loads enter
// the set. Without a probe, the declared product decides.
func servedEcosystemSet(d *documentpkg.Manifest, observed *workspace.Workspace) map[catalog.Ecosystem]struct{} {
	target := resolve.Target{Product: d.Server.Core.Product}
	served := map[catalog.Ecosystem]struct{}{}
	if observed != nil {
		for _, offer := range observed.ServedEcosystems() {
			if resolve.SupportsPackages(target, offer.Ecosystem) {
				served[offer.Ecosystem] = struct{}{}
			}
		}
	}
	if len(served) == 0 {
		if ecosystem := selectedEcosystem(d, catalog.EcosystemUnspecified); ecosystem != catalog.EcosystemUnspecified {
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
	served map[catalog.Ecosystem]struct{},
) (catalog.Ecosystem, bool) {
	for _, ecosystem := range slices.Sorted(maps.Keys(served)) {
		if ecosystem == catalog.EcosystemMCDR {
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
	return catalog.EcosystemUnspecified, false
}

// loaderSpecificDescriptor reports whether a module's descriptor names the
// loader that read it. Forge and NeoForge share META-INF/mods.toml, so only a
// loader dependency declared inside it identifies the loader.
func loaderSpecificDescriptor(module catalog.DescriptorModule) bool {
	if module.DescriptorPath != "META-INF/mods.toml" {
		return true
	}
	loader := module.Ecosystem.String()
	return slices.ContainsFunc(module.Dependencies, func(dep catalog.DescriptorDependency) bool {
		return dep.ID == loader
	})
}

func commandReference(d *documentpkg.Manifest, old *documentpkg.Lock, raw string, ecosystem catalog.Ecosystem) (string, version.Version, error) {
	request, err := input.Parse(raw)
	if err != nil {
		return "", "", err
	}
	provider, name := request.Provider, string(request.Name)
	if provider == catalog.ProviderUnspecified {
		if old != nil {
			ref, err := old.LookupReference(name)
			if err != nil {
				return "", "", err
			}
			if ref != "" {
				provider, name, err = documentpkg.ParseReference(ref)
				if err != nil {
					return "", "", err
				}
			}
		}
		if provider == catalog.ProviderUnspecified {
			provider = catalog.ProviderModrinth
			if ecosystem == catalog.EcosystemMCDR {
				provider = catalog.ProviderMCDR
			}
		}
	}
	ref := provider.String() + ":" + name
	if _, _, err := documentpkg.ParseReference(ref); err != nil {
		return "", "", err
	}
	selector := request.Version
	if selector == "" {
		selector = version.Any
	}
	return ref, selector, nil
}

func Add(ctx context.Context, root string, args []string, ecosystem catalog.Ecosystem, opts install.Options) (*install.Result, error) {
	d, old, err := commandDocuments(root)
	if err != nil {
		return nil, err
	}
	opts.ExpectedManifest, err = d.Fingerprint()
	if err != nil {
		return nil, err
	}
	opts.RequirementChange = true
	ecosystem = selectedEcosystem(d, ecosystem)
	if ecosystem == catalog.EcosystemUnspecified {
		return nil, fmt.Errorf("specify a package ecosystem supported by the declared server")
	}
	for _, raw := range args {
		ref, selector, err := commandReference(d, old, raw, ecosystem)
		if err != nil {
			return nil, err
		}
		r := documentpkg.Requirement{Selector: selector, Enabled: new(true)}
		if ecosystem == catalog.EcosystemMCDR {
			if d.MCDR == nil {
				return nil, fmt.Errorf("manifest has no MCDR runtime")
			}
			d.MCDR.Packages[ref] = r
		} else {
			if d.Server.Packages[ecosystem] == nil {
				d.Server.Packages[ecosystem] = map[string]documentpkg.Requirement{}
			}
			d.Server.Packages[ecosystem][ref] = r
		}
	}
	opts.PublishManifest = true
	return install.Sync(ctx, root, d, old, opts)
}

func Remove(ctx context.Context, root string, args []string, ecosystem catalog.Ecosystem, opts install.Options) (*install.Result, error) {
	d, old, err := commandDocuments(root)
	if err != nil {
		return nil, err
	}
	opts.ExpectedManifest, err = d.Fingerprint()
	if err != nil {
		return nil, err
	}
	opts.RequirementChange = true
	ecosystem = selectedEcosystem(d, ecosystem)
	for _, raw := range args {
		ref, _, err := commandReference(d, old, raw, ecosystem)
		if err != nil {
			return nil, err
		}
		packages := d.Server.Packages[ecosystem]
		if ecosystem == catalog.EcosystemMCDR {
			if d.MCDR == nil {
				return nil, fmt.Errorf("manifest has no MCDR runtime")
			}
			packages = d.MCDR.Packages
		}
		if _, ok := packages[ref]; !ok {
			return nil, fmt.Errorf("direct requirement %s not found in %s", ref, ecosystem)
		}
		delete(packages, ref)
	}
	opts.PublishManifest = true
	return install.Sync(ctx, root, d, old, opts)
}

func Initialize(ctx context.Context, root string, opts InitOptions) (result *InitResult, resultErr error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	writer, err := workspacewrite.Lock(root)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := workspacewrite.RecoverInstall(root); err != nil {
		return nil, err
	}
	if err := filetrack.RecoverPending(root); err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(root, documentpkg.ManifestFilename)); err == nil && !opts.Force {
		return nil, fmt.Errorf("lucy.yaml exists; use --force to replace declared intent")
	}
	d := &documentpkg.Manifest{
		SchemaVersion: documentpkg.ManifestSchemaVersion,
		Server: documentpkg.ManifestServer{
			Packages: map[catalog.Ecosystem]map[string]documentpkg.Requirement{},
		},
	}
	observed := workspace.Observe(root, discovery.Request{EntryPoint: opts.EntryPoint})
	if opts.EntryPoint == "" && observed.Runtime() == nil && opts.PromptRuntime != nil {
		var selectionErr *discovery.SelectionError
		if errors.As(observed.SelectionError(), &selectionErr) &&
			selectionErr.State == discovery.SelectionAmbiguous {
			entryPoint, promptErr := opts.PromptRuntime(observed.Discovery)
			if promptErr != nil {
				return initResult(observed), promptErr
			}
			if entryPoint != "" {
				if !filepath.IsAbs(entryPoint) {
					entryPoint = filepath.Join(root, entryPoint)
				}
				if err := observed.Select(discovery.Request{EntryPoint: entryPoint}); err != nil {
					return initResult(observed), err
				}
				observed.ObservePackages()
			}
		}
	}
	if selection := observed.Runtime(); selection != nil {
		identity := selection.Installation.Identity
		d.Server.Minecraft = selection.MinecraftVersion()
		if detected := selection.Product(); detected != serverpkg.ProductUnknown {
			d.Server.Core.Product = detected
		} else {
			d.Server.Core.Product = serverpkg.Product(identity.Primary.Name.String())
		}
		if identity.Primary.Version != "" &&
			!identity.Primary.Version.IsInvalid() &&
			!identity.Primary.Version.CanInfer() {
			d.Server.Core.ProductVersion = identity.Primary.Version
		}
		if paperLineageProduct(d.Server.Core.Product) &&
			!concreteVersion(d.Server.Core.ProductVersion) {
			build, found, err := identifyInstalledCoreBuild(
				ctx,
				root,
				selection,
				d.Server.Core.Product,
			)
			if err != nil {
				return initResult(observed), err
			}
			if !found {
				return initResult(observed), fmt.Errorf(
					"cannot identify the installed %s build; restore the "+
						"published %s-<minecraft>-<build>.jar name or pin "+
						"the core version in lucy.yaml",
					d.Server.Core.Product,
					d.Server.Core.Product,
				)
			}
			d.Server.Core.ProductVersion = build
		}
		if observed.ServerDir == "" {
			d.Server.Directory = "."
		} else {
			relative, err := filepath.Rel(root, observed.ServerDir)
			if err != nil {
				return initResult(observed), err
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
	} else {
		if selectionErr := observed.SelectionError(); selectionErr != nil {
			var typed *discovery.SelectionError
			noServer := errors.As(selectionErr, &typed) &&
				(typed.State == discovery.SelectionNone ||
					(typed.State == discovery.SelectionInvalid && typed.Path == ""))
			if !noServer {
				return initResult(observed), selectionErr
			}
		}
		return initResult(observed), fmt.Errorf(
			"no server detected; use `lucy create` to generate a new server",
		)
	}
	observed, _ = identify.Packages(observed)
	if d.Server.Minecraft == "" || d.Server.Minecraft.IsInvalid() {
		return initResult(observed), fmt.Errorf(
			"the detected server has no concrete Minecraft version",
		)
	}
	if d.Server.Core.Product == serverpkg.ProductUnknown {
		d.Server.Core.Product = serverpkg.ProductMinecraft
		log.Debug("core product defaulted", "product", d.Server.Core.Product)
	}
	mcdrVersion := opts.MCDRVersion
	if mcdrVersion == "" && observed != nil && observed.Wrappers.MCDR != nil {
		v := observed.Wrappers.MCDR.Version
		if v.IsInvalid() || v.CanInfer() {
			return initResult(observed), fmt.Errorf("MCDR detected without an exact app version; specify --mcdr-version")
		}
		mcdrVersion = string(v)
		log.Debug("MCDR version from workspace detection", "version", mcdrVersion)
	}
	if mcdrVersion != "" {
		d.MCDR = &documentpkg.ManifestMCDR{Directory: ".", Version: version.Version(mcdrVersion), Packages: map[string]documentpkg.Requirement{}}
	}
	if err := d.Normalize(); err != nil {
		return initResult(observed), err
	}
	modrinthMapper, hasModrinthMapper, err := routing.GetArtifactMapper(catalog.ProviderModrinth)
	if err != nil {
		return initResult(observed), err
	}
	served := servedEcosystemSet(d, observed)
	var packages []workspace.DiscoveredPackage
	if observed != nil {
		packages = observed.Packages
	}
	recognized := 0
	for _, p := range packages {
		if !artifact.SupportsPath(p.Path) {
			continue
		}
		ecosystem := p.ID.Ecosystem
		if ecosystem != catalog.EcosystemMCDR {
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
		if ecosystem == catalog.EcosystemMCDR && d.MCDR != nil {
			candidates, err := routing.Candidates(ctx, catalog.RegistryMCDR, modules[0].ID, string(d.Server.Minecraft), ecosystem)
			if err != nil {
				continue
			}
			for _, candidate := range candidates {
				if candidate.Hashes["sha256"] == "" {
					continue
				}
				if _, err := install.DigestFile(p.Path, candidate.Hashes); err == nil {
					d.MCDR.Packages["mcdr:"+candidate.ProjectID] = documentpkg.Requirement{Selector: candidate.Version}
					recognized++
					break
				}
			}
			continue
		}
		if ecosystem != catalog.EcosystemFabric && ecosystem != catalog.EcosystemForge && ecosystem != catalog.EcosystemNeoForge && ecosystem != catalog.EcosystemBukkit && ecosystem != catalog.EcosystemPaper {
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
			ctx, catalog.RegistryModrinth, ref.Name.String(),
			string(d.Server.Minecraft), ecosystem,
		)
		usable := candidateErr == nil && slices.ContainsFunc(
			candidates,
			func(candidate upstream.Candidate) bool {
				return candidate.Version == ref.Version && candidate.PrimaryFile
			},
		)
		if !usable {
			if candidateErr == nil {
				candidateErr = fmt.Errorf("no release provides version %s", ref.Version)
			}
			if observed != nil {
				observed.Warnings = append(observed.Warnings, fmt.Errorf("package %s left unmanaged: %w", ref.Name, candidateErr))
			}
			continue
		}
		if d.Server.Packages[ecosystem] == nil {
			d.Server.Packages[ecosystem] = map[string]documentpkg.Requirement{}
		}
		d.Server.Packages[ecosystem]["modrinth:"+ref.Name.String()] = documentpkg.Requirement{Selector: ref.Version}
		recognized++
	}
	log.Debug(
		"recognized existing packages",
		"count", recognized,
		"present", len(packages),
	)
	old, err := documentpkg.LockFile(root).Read()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return initResult(observed), err
	}
	if errors.Is(err, os.ErrNotExist) {
		old = nil
	}
	// Adoption records the set the server already runs, so module
	// requirements that the installed jars report but the catalogue metadata
	// cannot satisfy become warnings instead of a refusal.
	resolved, resolution, err := install.Resolve(
		ctx,
		root,
		d,
		old,
		install.Options{AcceptBroken: true, Trace: opts.Trace},
	)
	if observed != nil && resolution != nil {
		observed.Warnings = append(observed.Warnings, resolution.Warnings...)
	}
	if err != nil {
		return initResult(observed), err
	}
	if err := documentpkg.ManifestFile(root).Write(d); err != nil {
		return initResult(observed), err
	}
	if resolved != old {
		if err := documentpkg.LockFile(root).Write(resolved); err != nil {
			return initResult(observed), err
		}
	}
	result = &InitResult{
		Document:      d,
		Lock:          resolved,
		PublishedLock: resolved != old,
	}
	if observed != nil {
		result.Warnings = observed.Warnings
		result.Discovery = observed.Discovery
	}
	return result, nil
}

// initResult carries the observations of a failed initialization so the
// command boundary can present them alongside the failure.
func initResult(observed *workspace.Workspace) *InitResult {
	if observed == nil {
		return nil
	}
	return &InitResult{
		Warnings:  observed.Warnings,
		Discovery: observed.Discovery,
	}
}

func concreteVersion(v version.Version) bool {
	return v != "" && !v.IsInvalid() && !v.CanInfer()
}

func paperLineageProduct(product serverpkg.Product) bool {
	switch product {
	case serverpkg.ProductPaper, serverpkg.ProductPurpur, serverpkg.ProductFolia:
		return true
	default:
		return false
	}
}

func identifyInstalledCoreBuild(
	ctx context.Context,
	root string,
	selection *discovery.Selection,
	product serverpkg.Product,
) (version.Version, bool, error) {
	if selection == nil || selection.EntryPoint == nil || selection.EntryPoint.Path == "" {
		return "", false, nil
	}
	path := selection.EntryPoint.Path
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	file, err := os.Open(path)
	if err != nil {
		return "", false, fmt.Errorf("read %s jar: %w", product, err)
	}
	sha := sha256.New()
	md := md5.New()
	_, copyErr := io.Copy(io.MultiWriter(sha, md), file)
	closeErr := file.Close()
	if copyErr != nil {
		return "", false, copyErr
	}
	if closeErr != nil {
		return "", false, closeErr
	}
	var build version.Version
	var found bool
	switch product {
	case serverpkg.ProductPaper:
		build, found, err = routing.IdentifyFillBuild(
			ctx,
			"paper",
			selection.MinecraftVersion(),
			hex.EncodeToString(sha.Sum(nil)),
		)
	case serverpkg.ProductFolia:
		build, found, err = routing.IdentifyFillBuild(
			ctx,
			"folia",
			selection.MinecraftVersion(),
			hex.EncodeToString(sha.Sum(nil)),
		)
	case serverpkg.ProductPurpur:
		build, found, err = routing.IdentifyPurpurBuild(
			ctx,
			selection.MinecraftVersion(),
			hex.EncodeToString(md.Sum(nil)),
		)
	}
	if err != nil {
		return "", false, fmt.Errorf("identify installed %s build: %w", product, err)
	}
	return build, found, nil
}
