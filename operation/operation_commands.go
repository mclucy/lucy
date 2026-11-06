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
	"strings"

	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/artifactstore"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/config"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/resolve"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
	"github.com/mclucy/lucy/workspace/identify"
)

// InitOptions describes adoption of the server already present in a
// directory.
type InitOptions struct {
	Name        string
	MCDRVersion string
	Force       bool
	NoComments  bool
	// EntryPoint selects the runtime observation uses for this
	// initialization only; it is never persisted.
	EntryPoint string
	// PromptRuntime asks which entry point to use when observation finds
	// several equally valid runtimes. Nil keeps the strict failure.
	PromptRuntime func(discovery.Result) (string, error)
	// Trace receives progress for the lock resolution initialization runs.
	Trace *install.Trace
	// Materialize supplies artifact bytes for adoption planning. Nil uses the
	// configured cache and network.
	Materialize func(*catalog.Artifact) (artifact.MaterializedFile, error)
}

// InitResult is the structured outcome of one initialization. The document
// is the written manifest; warnings and discovery carry the observations
// that the command boundary presents.
type InitResult struct {
	Document        *documentpkg.Manifest
	Lock            *documentpkg.Lock
	PublishedLock   bool
	PublishedConfig bool
	Warnings        []error
	Discovery       discovery.Result
}

func commandDocuments(root string, logger *log.Logger) (document *documentpkg.Manifest, locked *documentpkg.Lock, resultErr error) {
	writer, err := workspacewrite.Lock(root, logger)
	if err != nil {
		return nil, nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := filetrack.RecoverLocked(root, logger); err != nil {
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

// pinWorkspaceDocuments fills the state an operation selected from when the
// caller did not already pin it. The installation re-locks the workspace, so a
// recovery or concurrent edit in between fails instead of being overwritten.
func pinWorkspaceDocuments(opts *install.Options, d *documentpkg.Manifest, old *documentpkg.Lock) error {
	if d != nil && opts.ExpectedManifest == "" {
		fingerprint, err := d.Fingerprint()
		if err != nil {
			return err
		}
		opts.ExpectedManifest = fingerprint
	}
	if old == nil || opts.ExpectedLock != "" {
		return nil
	}
	fingerprint, err := old.Fingerprint()
	if err != nil {
		return err
	}
	opts.ExpectedLock = fingerprint
	return nil
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

func (s *Service) Add(ctx context.Context, root string, args []string, ecosystem catalog.Ecosystem, opts install.Options) (*install.Result, error) {
	d, old, err := commandDocuments(root, s.logger)
	if err != nil {
		return nil, err
	}
	if err := pinWorkspaceDocuments(&opts, d, old); err != nil {
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
	return s.install.Sync(ctx, root, d, old, opts)
}

func (s *Service) Remove(ctx context.Context, root string, args []string, ecosystem catalog.Ecosystem, opts install.Options) (*install.Result, error) {
	d, old, err := commandDocuments(root, s.logger)
	if err != nil {
		return nil, err
	}
	if err := pinWorkspaceDocuments(&opts, d, old); err != nil {
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
	return s.install.Sync(ctx, root, d, old, opts)
}

func (s *Service) Initialize(ctx context.Context, root string, opts InitOptions) (result *InitResult, resultErr error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	lease, err := artifactstore.AcquireOperation()
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, lease.Close()) }()
	writer, err := workspacewrite.Lock(root, s.logger)
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, writer.Close()) }()
	if err := filetrack.RecoverLocked(root, s.logger); err != nil {
		return nil, err
	}
	// Pin the recovered documents before resolution reads the workspace.
	// Adoption publishes the manifest, lock, and receipt as one transaction
	// and refuses to overwrite bytes that changed in between.
	preconditions, err := install.CaptureAdoptionPreconditions(root)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(filepath.Join(root, documentpkg.ManifestFilename)); err == nil && !opts.Force {
		return nil, fmt.Errorf("lucy.yaml exists; use --force to replace declared intent")
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" && opts.Force {
		// Replacing a declaration keeps the name the user already chose; an
		// unreadable or nameless manifest falls back to the directory name.
		if existing, err := documentpkg.ManifestFile(root).Read(); err == nil {
			name = existing.Name
		}
	}
	if name == "" {
		name = filepath.Base(root)
	}
	d := &documentpkg.Manifest{
		SchemaVersion: documentpkg.ManifestSchemaVersion,
		Name:          name,
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
			}
		}
	}
	// Bind the selected runtime bytes before identification performs network
	// work. Adoption may only record these bytes; verifyRuntimeObservation
	// rejects a replaced runtime instead of adopting stale membership.
	runtimeObserved, err := captureRuntimeObservation(observed.Runtime())
	if err != nil {
		return initResult(observed), err
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
			build, found, err := s.identifyInstalledCoreBuild(
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
		s.logger.Debug(
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
	var identificationErrors []error
	observed, identificationErrors = identify.Packages(ctx, observed, s.source.ResolveArtifactByHash)
	if err := ctx.Err(); err != nil {
		return initResult(observed), err
	}
	for _, err := range identificationErrors {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return initResult(observed), err
		}
	}
	if d.Server.Minecraft == "" || d.Server.Minecraft.IsInvalid() {
		return initResult(observed), fmt.Errorf(
			"the detected server has no concrete Minecraft version",
		)
	}
	if d.Server.Core.Product == serverpkg.ProductUnknown {
		d.Server.Core.Product = serverpkg.ProductMinecraft
		s.logger.Debug("core product defaulted", "product", d.Server.Core.Product)
	}
	mcdrVersion := opts.MCDRVersion
	if mcdrVersion == "" && observed != nil && observed.Wrappers.MCDR != nil {
		v := observed.Wrappers.MCDR.Version
		if v.IsInvalid() || v.CanInfer() {
			return initResult(observed), fmt.Errorf("MCDR detected without an exact app version; specify --mcdr-version")
		}
		mcdrVersion = string(v)
		s.logger.Debug("MCDR version from workspace detection", "version", mcdrVersion)
	}
	if mcdrVersion != "" {
		d.MCDR = &documentpkg.ManifestMCDR{Directory: ".", Version: version.Version(mcdrVersion), Packages: map[string]documentpkg.Requirement{}}
	}
	if err := d.Normalize(); err != nil {
		return initResult(observed), err
	}
	modrinthMapper, hasModrinthMapper, err := s.source.GetArtifactMapper(catalog.ProviderModrinth)
	if err != nil {
		return initResult(observed), err
	}
	served := servedEcosystemSet(d, observed)
	var packages []workspace.DiscoveredPackage
	if observed != nil {
		packages = observed.Packages
	}
	recognized := 0
	adoptedPackages := []install.AdoptedPackage{}
	// A manifest requirement names one project, so two installed jars that
	// identify the same Modrinth project cannot both be managed: the key would
	// otherwise record only the last file while the receipt and fingerprint
	// claim both. Conflicting projects stay unmanaged.
	adoptedPackageIndex := make(map[string]int)
	adoptedReferences := make(map[string]string)
	conflictedPackages := make(map[string]bool)
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
			pluginDirectory := filepath.Join(root, d.MCDR.Directory, "plugins")
			if filepath.Clean(filepath.Dir(p.Path)) != filepath.Clean(pluginDirectory) {
				observed.Warnings = append(observed.Warnings, fmt.Errorf(
					"plugin %s left unmanaged: MCDReforged loads it outside the default plugins directory",
					filepath.Base(p.Path),
				))
				continue
			}
			candidates, err := s.source.Candidates(ctx, catalog.RegistryMCDR, modules[0].ID, string(d.Server.Minecraft), ecosystem)
			if err != nil {
				continue
			}
			for _, candidate := range candidates {
				if candidate.Hashes["sha256"] == "" {
					continue
				}
				if _, err := install.DigestFile(p.Path, candidate.Hashes); err == nil {
					info, statErr := os.Lstat(p.Path)
					if statErr != nil {
						return initResult(observed), fmt.Errorf(
							"inspect package %s: %w", p.Path, statErr,
						)
					}
					packageID := "mcdr:" + candidate.ProjectID
					projectKey := string(ecosystem) + "\x00" + packageID
					if conflictedPackages[projectKey] {
						if observed != nil {
							observed.Warnings = append(observed.Warnings, fmt.Errorf(
								"package %s left unmanaged: several installed plugins identify the same project",
								candidate.ProjectID,
							))
						}
						break
					}
					if index, exists := adoptedPackageIndex[projectKey]; exists {
						conflictedPackages[projectKey] = true
						delete(adoptedPackageIndex, projectKey)
						delete(d.MCDR.Packages, packageID)
						priorPath := ""
						if index < len(adoptedPackages) {
							priorPath = adoptedPackages[index].Path
							adoptedPackages[index] = install.AdoptedPackage{}
						}
						recognized--
						if observed != nil {
							observed.Warnings = append(observed.Warnings, fmt.Errorf(
								"package %s left unmanaged: installed plugins %s and %s identify the same project",
								candidate.ProjectID, priorPath, p.Path,
							))
						}
						break
					}
					d.MCDR.Packages[packageID] = documentpkg.Requirement{Selector: candidate.Version}
					recognized++
					adoptedPackageIndex[projectKey] = len(adoptedPackages)
					adoptedPackages = append(adoptedPackages, install.AdoptedPackage{
						Path:           p.Path,
						PackageID:      packageID,
						ExpectedSHA256: candidate.Hashes["sha256"],
						ExpectedMode:   uint32(info.Mode().Perm()),
					})
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
		fileDigests, digestErr := install.DigestFile(p.Path, nil)
		if digestErr != nil {
			return initResult(observed), fmt.Errorf(
				"digest package %s: %w", p.Path, digestErr,
			)
		}
		fileInfo, statErr := os.Lstat(p.Path)
		if statErr != nil {
			return initResult(observed), fmt.Errorf(
				"inspect package %s: %w", p.Path, statErr,
			)
		}
		ref, _, matched, err := modrinthMapper.PackageByHash(ctx, artifact.File{Path: p.Path})
		if err != nil || !matched || ref.Name == "" {
			continue
		}
		// A manifest entry that resolution cannot select blocks every later
		// command, so an installed package whose catalogue metadata offers no
		// matching release for this server stays unmanaged.
		candidates, candidateErr := s.source.Candidates(
			ctx, catalog.RegistryModrinth, ref.Name.String(),
			string(d.Server.Minecraft), ecosystem,
		)
		candidateIndex := -1
		if candidateErr == nil {
			candidateIndex = slices.IndexFunc(candidates, func(candidate upstream.Candidate) bool {
				return candidate.Version == ref.Version && candidate.PrimaryFile
			})
		}
		if candidateIndex < 0 {
			if candidateErr == nil {
				candidateErr = fmt.Errorf("no release provides version %s", ref.Version)
			}
			if observed != nil {
				observed.Warnings = append(observed.Warnings, fmt.Errorf("package %s left unmanaged: %w", ref.Name, candidateErr))
			}
			continue
		}
		projectID := candidates[candidateIndex].ProjectID
		if projectID == "" {
			observed.Warnings = append(observed.Warnings, fmt.Errorf(
				"package %s left unmanaged: matched release has no project ID", ref.Name,
			))
			continue
		}
		reference := "modrinth:" + ref.Name.String()
		packageID := "modrinth:" + projectID
		projectKey := string(ecosystem) + "\x00" + packageID
		if conflictedPackages[projectKey] {
			if observed != nil {
				observed.Warnings = append(observed.Warnings, fmt.Errorf(
					"package %s left unmanaged: several installed jars identify the same project",
					ref.Name,
				))
			}
			continue
		}
		if index, exists := adoptedPackageIndex[projectKey]; exists {
			conflictedPackages[projectKey] = true
			delete(adoptedPackageIndex, projectKey)
			delete(d.Server.Packages[ecosystem], adoptedReferences[projectKey])
			delete(adoptedReferences, projectKey)
			priorPath := ""
			if index < len(adoptedPackages) {
				priorPath = adoptedPackages[index].Path
				adoptedPackages[index] = install.AdoptedPackage{}
			}
			recognized--
			if observed != nil {
				observed.Warnings = append(observed.Warnings, fmt.Errorf(
					"package %s left unmanaged: installed jars %s and %s identify the same project",
					ref.Name, priorPath, p.Path,
				))
			}
			continue
		}
		if d.Server.Packages[ecosystem] == nil {
			d.Server.Packages[ecosystem] = map[string]documentpkg.Requirement{}
		}
		d.Server.Packages[ecosystem][reference] = documentpkg.Requirement{Selector: ref.Version}
		adoptedReferences[projectKey] = reference
		recognized++
		adoptedPackageIndex[projectKey] = len(adoptedPackages)
		adoptedPackages = append(adoptedPackages, install.AdoptedPackage{
			Path:           p.Path,
			PackageID:      packageID,
			ExpectedSHA256: fileDigests["sha256"],
			ExpectedMode:   uint32(fileInfo.Mode().Perm()),
		})
	}
	adoptedPackages = slices.DeleteFunc(
		adoptedPackages,
		func(pkg install.AdoptedPackage) bool { return pkg.Path == "" },
	)
	s.logger.Debug(
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
	resolved, resolution, err := s.install.Resolve(
		ctx,
		root,
		d,
		old,
		install.Options{AcceptBroken: true, Trace: opts.Trace, Materialize: opts.Materialize},
	)
	if observed != nil && resolution != nil {
		observed.Warnings = append(observed.Warnings, resolution.Warnings...)
	}
	if err != nil {
		return initResult(observed), err
	}
	configurationPath := filepath.Join(root, filepath.FromSlash(config.WorkspaceFilename))
	if err := workspacewrite.SafePath(root, config.WorkspaceFilename); err != nil {
		return initResult(observed), err
	}
	_, configErr := os.Lstat(configurationPath)
	publishedConfig := errors.Is(configErr, os.ErrNotExist)
	if configErr != nil && !publishedConfig {
		return initResult(observed), configErr
	}
	if err := ctx.Err(); err != nil {
		return initResult(observed), err
	}
	var selection *discovery.Selection
	if observed != nil {
		selection = observed.Runtime()
	}
	if runtimeObserved != nil {
		// Re-discover from disk instead of trusting the in-memory selection
		// that may predate the ambiguity prompt or network identification.
		fresh := workspace.Observe(
			root,
			discovery.Request{EntryPoint: runtimeObserved.entryPoint},
		)
		freshSelection := fresh.Runtime()
		if err := runtimeObserved.verify(freshSelection); err != nil {
			return initResult(observed), err
		}
		selection = freshSelection
	}
	var runtimeFiles []install.AdoptedRuntimeFile
	if runtimeObserved != nil {
		runtimeFiles = runtimeObserved.files
	}
	manifestFile := documentpkg.ManifestFile(root)
	if opts.Force {
		manifestFile = manifestFile.WithoutMerge()
	}
	if opts.NoComments {
		manifestFile = manifestFile.WithEncoder((*documentpkg.Manifest).PlainBytes)
	}
	var manifestMerge func([]byte) ([]byte, error)
	if !opts.Force && !opts.NoComments {
		manifestMerge = d.MergeBytes
	}
	publishLock := resolved != old
	// Adoption publishes the manifest, lock, and receipt as one workspace
	// transaction. Registration runs only after that commit because the
	// registry reads the manifest, and no document is written ahead of the
	// transaction. The operation lease stays held until registration
	// completes: the artifact store's sweep cannot reclaim the bytes
	// resolution materialized while the workspace still had no visible lock.
	var committedErr *install.CommittedError
	adoptErr := install.AdoptExistingWorkspace(root, install.AdoptOptions{
		Manifest:     d,
		Lock:         resolved,
		Runtime:      selection,
		RuntimeFiles: runtimeFiles,
		Packages:     adoptedPackages,
		Logger:       s.logger,
		PublishLock:  publishLock,
		ManifestSnapshot: func() ([]byte, error) {
			return manifestFile.Snapshot(d)
		},
		ManifestMerge: manifestMerge,
		Preconditions: preconditions,
	})
	if adoptErr != nil && !errors.As(adoptErr, &committedErr) {
		return initResult(observed), adoptErr
	}
	result = &InitResult{
		Document:      d,
		Lock:          resolved,
		PublishedLock: publishLock,
	}
	if observed != nil {
		result.Warnings = observed.Warnings
		result.Discovery = observed.Discovery
	}
	// Registration is required even when the transaction committed but its
	// cleanup or post-commit verification failed: the published lock must stay
	// traceable by the artifact store's sweep. The original committed error is
	// returned after registration, never replaced by it.
	duplicates, regErr := s.registerWorkspace(root)
	if regErr != nil {
		regErr = fmt.Errorf("register server: %w", regErr)
		if adoptErr != nil {
			return result, errors.Join(adoptErr, regErr)
		}
		return result, regErr
	}
	for _, duplicate := range duplicates {
		result.Warnings = append(result.Warnings, fmt.Errorf(
			"server name %q is also registered at %s; --server cannot select an ambiguous name",
			duplicate.Name,
			duplicate.Path,
		))
	}
	if adoptErr != nil {
		// The manifest, lock, and receipt reached the commit point. Do not
		// publish user configuration past a failed transaction completion.
		return result, adoptErr
	}
	if publishedConfig {
		created, err := config.CreateWorkspace(root, opts.NoComments)
		if err != nil {
			return result, err
		}
		publishedConfig = created
	}
	result.PublishedConfig = publishedConfig
	return result, nil
}

// runtimeObservation binds the selected runtime identity, membership, and
// regular-file bytes observed before identification performs network work.
// Adoption may only publish these bytes.
type runtimeObservation struct {
	entryPoint  string
	product     serverpkg.Product
	minecraft   version.Version
	coreVersion version.Version
	members     map[string]discovery.FileRole
	files       []install.AdoptedRuntimeFile
}

func captureRuntimeObservation(
	selection *discovery.Selection,
) (*runtimeObservation, error) {
	if selection == nil || selection.Installation == nil {
		return nil, nil
	}
	files, err := adoptedRuntimeFiles(selection)
	if err != nil {
		return nil, err
	}
	members := make(map[string]discovery.FileRole, len(selection.Installation.Files))
	for _, file := range selection.Installation.Files {
		members[file.Path] = file.Role
	}
	entryPoint := ""
	if selection.EntryPoint != nil {
		entryPoint = selection.EntryPoint.Path
	}
	return &runtimeObservation{
		entryPoint:  entryPoint,
		product:     selection.Product(),
		minecraft:   selection.MinecraftVersion(),
		coreVersion: selection.Installation.Identity.Primary.Version,
		members:     members,
		files:       files,
	}, nil
}

// verify rejects a runtime that changed after observation. The caller passes a
// fresh discovery result, and verify rechecks the selected entry point,
// identity, membership roles, and every observed file's digest and mode, so
// inconclusive adoption cannot record ownership of a replacement using stale
// membership.
func (o *runtimeObservation) verify(selection *discovery.Selection) error {
	if o == nil {
		if selection != nil {
			return errors.New("a runtime appeared after observation; rerun lucy init")
		}
		return nil
	}
	if selection == nil || selection.Installation == nil {
		return errors.New("the observed runtime disappeared during initialization")
	}
	entryPoint := ""
	if selection.EntryPoint != nil {
		entryPoint = selection.EntryPoint.Path
	}
	if entryPoint != o.entryPoint ||
		selection.Product() != o.product ||
		selection.MinecraftVersion() != o.minecraft ||
		selection.Installation.Identity.Primary.Version != o.coreVersion {
		return errors.New("the selected runtime identity changed during initialization")
	}
	if len(selection.Installation.Files) != len(o.members) {
		return errors.New("the selected runtime membership changed during initialization")
	}
	for _, file := range selection.Installation.Files {
		role, ok := o.members[file.Path]
		if !ok || role != file.Role {
			return errors.New("the selected runtime membership changed during initialization")
		}
	}
	for _, observed := range o.files {
		info, err := os.Lstat(observed.Path)
		if err != nil || !info.Mode().IsRegular() {
			return fmt.Errorf(
				"runtime file %s changed during initialization",
				observed.Path,
			)
		}
		hashes, err := install.DigestFile(observed.Path, nil)
		if err != nil {
			return fmt.Errorf(
				"digest runtime file %s: %w",
				observed.Path,
				err,
			)
		}
		if hashes["sha256"] != observed.ExpectedSHA256 ||
			uint32(info.Mode().Perm()) != observed.ExpectedMode {
			return fmt.Errorf(
				"runtime file %s changed during initialization",
				observed.Path,
			)
		}
	}
	return nil
}

// adoptedRuntimeFiles captures the digest and mode of every file the selected
// runtime owns. Adoption records only files with a captured baseline, so an
// unreadable or replaced file fails initialization instead of being adopted
// without evidence.
func adoptedRuntimeFiles(
	selection *discovery.Selection,
) ([]install.AdoptedRuntimeFile, error) {
	if selection == nil || selection.Installation == nil {
		return nil, nil
	}
	files := make([]install.AdoptedRuntimeFile, 0, len(selection.Installation.Files))
	for _, f := range selection.Installation.Files {
		info, err := os.Lstat(f.Path)
		if err != nil {
			return nil, fmt.Errorf("inspect runtime file %s: %w", f.Path, err)
		}
		if !info.Mode().IsRegular() {
			if info.Mode()&os.ModeSymlink != 0 {
				// A launcher alias stays unowned; discovery selects the
				// canonical target when one exists.
				continue
			}
			return nil, fmt.Errorf("runtime file %s is not a regular file", f.Path)
		}
		hashes, err := install.DigestFile(f.Path, nil)
		if err != nil {
			return nil, fmt.Errorf("digest runtime file %s: %w", f.Path, err)
		}
		files = append(files, install.AdoptedRuntimeFile{
			Path:           f.Path,
			Role:           f.Role,
			ExpectedSHA256: hashes["sha256"],
			ExpectedMode:   uint32(info.Mode().Perm()),
		})
	}
	return files, nil
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

func (s *Service) identifyInstalledCoreBuild(
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
		build, found, err = s.source.IdentifyFillBuild(
			ctx,
			"paper",
			selection.MinecraftVersion(),
			hex.EncodeToString(sha.Sum(nil)),
		)
	case serverpkg.ProductFolia:
		build, found, err = s.source.IdentifyFillBuild(
			ctx,
			"folia",
			selection.MinecraftVersion(),
			hex.EncodeToString(sha.Sum(nil)),
		)
	case serverpkg.ProductPurpur:
		build, found, err = s.source.IdentifyPurpurBuild(
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
