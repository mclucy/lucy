package operation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/artifactstore"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/config"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/workspacewrite"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
	"github.com/mclucy/lucy/workspace/identify"
)

// Initialize adopts the server already present in root: it observes the
// runtime and packages, resolves the lock, and publishes the manifest, lock,
// and installation receipt as one workspace transaction.
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

	manifest, err := initialManifest(root, opts)
	if err != nil {
		return nil, err
	}
	observed := workspace.Observe(
		root,
		discovery.Request{Expectation: opts.Expectation},
	)
	if err := promptRuntime(opts, observed); err != nil {
		return initResult(observed), err
	}
	// Bind the selected runtime bytes before identification performs network
	// work. Adoption may only record these bytes; the observation rejects a
	// replaced runtime instead of adopting stale membership.
	var runtimeInstallation *discovery.Installation
	if selection := observed.Runtime(); selection != nil {
		runtimeInstallation = selection.Installation
	}
	runtimeObserved, err := captureRuntimeObservation(runtimeInstallation)
	if err != nil {
		return initResult(observed), err
	}
	if err := s.applyRuntimeIdentity(ctx, root, manifest, observed); err != nil {
		return initResult(observed), err
	}
	if err := verifyCoreVersionExpectation(opts.Expectation, manifest); err != nil {
		return initResult(observed), err
	}

	var identificationErrors []error
	observed, identificationErrors = identify.Packages(
		ctx,
		observed,
		s.source.ResolveArtifactByHash,
	)
	if err := ctx.Err(); err != nil {
		return initResult(observed), err
	}
	if err := identificationFailure(identificationErrors); err != nil {
		return initResult(observed), err
	}
	if err := s.applyDetectedVersions(manifest, opts, observed); err != nil {
		return initResult(observed), err
	}
	if err := manifest.Normalize(); err != nil {
		return initResult(observed), err
	}
	adoptedPackages, err := s.adoptInstalledPackages(ctx, root, manifest, observed)
	if err != nil {
		return initResult(observed), err
	}
	old, err := readExistingLock(root)
	if err != nil {
		return initResult(observed), err
	}
	resolved, err := s.resolveAdoptedLock(ctx, root, manifest, old, opts, observed)
	if err != nil {
		return initResult(observed), err
	}
	return s.publishInitialization(
		ctx,
		root,
		manifest,
		old,
		resolved,
		adoptedPackages,
		runtimeObserved,
		observed,
		opts,
		preconditions,
	)
}

// verifyCoreVersionExpectation confirms that the version identity assigned
// after selection answers an exact core version filter. Selection accepts an
// installation whose observed version is unknown; identification can then
// reveal a different build, which must fail the invocation instead of
// publishing a server the caller did not request.
func verifyCoreVersionExpectation(
	expectation *discovery.Expectation,
	manifest *documentpkg.Manifest,
) error {
	if expectation == nil || !concreteVersion(expectation.CoreVersion) {
		return nil
	}
	identified := manifest.Server.Core.ProductVersion
	if identified == expectation.CoreVersion {
		return nil
	}
	if !concreteVersion(identified) {
		return fmt.Errorf(
			"cannot identify the installed %s version, which the invocation requires to be %s",
			manifest.Server.Core.Product,
			expectation.CoreVersion,
		)
	}
	return fmt.Errorf(
		"installed %s version %s does not match the requested %s",
		manifest.Server.Core.Product,
		identified,
		expectation.CoreVersion,
	)
}

// initialManifest builds the declaration initialization will write, keeping
// the name an existing declaration already chose when --force replaces it.
func initialManifest(root string, opts InitOptions) (*documentpkg.Manifest, error) {
	if _, err := os.Stat(filepath.Join(root, documentpkg.ManifestFilename)); err == nil &&
		!opts.Force {
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
	return &documentpkg.Manifest{
		SchemaVersion: documentpkg.ManifestSchemaVersion,
		Name:          name,
		Server: documentpkg.ManifestServer{
			Packages: map[catalog.Ecosystem]map[string]documentpkg.Requirement{},
		},
	}, nil
}

// promptRuntime asks the caller to disambiguate several equally valid
// runtimes and applies the selection to observed.
func promptRuntime(opts InitOptions, observed *workspace.Workspace) error {
	if opts.Expectation != nil || observed.Runtime() != nil || opts.PromptRuntime == nil {
		return nil
	}
	var selectionErr *discovery.SelectionError
	if !errors.As(observed.SelectionError(), &selectionErr) ||
		selectionErr.State != discovery.SelectionAmbiguous {
		return nil
	}
	installation, err := opts.PromptRuntime(observed.Discovery)
	if err != nil {
		return err
	}
	if installation == nil {
		return nil
	}
	return observed.Select(discovery.Request{Installation: installation})
}

// applyRuntimeIdentity records the observed server's product, versions, and
// directory in the manifest.
func (s *Service) applyRuntimeIdentity(
	ctx context.Context,
	root string,
	manifest *documentpkg.Manifest,
	observed *workspace.Workspace,
) error {
	selection := observed.Runtime()
	if selection == nil {
		if selectionErr := observed.SelectionError(); selectionErr != nil {
			var typed *discovery.SelectionError
			noServer := errors.As(selectionErr, &typed) &&
				typed.State == discovery.SelectionNone
			if !noServer {
				return selectionErr
			}
		}
		return fmt.Errorf(
			"no server detected; use `lucy create` to generate a new server",
		)
	}

	identity := selection.Installation.Identity
	manifest.Server.Minecraft = selection.MinecraftVersion()
	if detected := selection.Product(); detected != serverpkg.ProductUnknown {
		manifest.Server.Core.Product = detected
	} else {
		manifest.Server.Core.Product = serverpkg.Product(
			identity.Primary.Name.String(),
		)
	}
	if identity.Primary.Version != "" &&
		!identity.Primary.Version.IsInvalid() &&
		!identity.Primary.Version.CanInfer() {
		manifest.Server.Core.ProductVersion = identity.Primary.Version
	}
	if paperLineageProduct(manifest.Server.Core.Product) &&
		!concreteVersion(manifest.Server.Core.ProductVersion) {
		build, found, err := s.identifyInstalledCoreBuild(
			ctx,
			root,
			selection.Installation,
			manifest.Server.Core.Product,
		)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf(
				"cannot identify the installed %s build; restore the "+
					"published %s-<minecraft>-<build>.jar name or pin "+
					"the core version in lucy.yaml",
				manifest.Server.Core.Product,
				manifest.Server.Core.Product,
			)
		}
		manifest.Server.Core.ProductVersion = build
	}
	if observed.ServerDir == "" {
		manifest.Server.Directory = "."
	} else {
		relative, err := filepath.Rel(root, observed.ServerDir)
		if err != nil {
			return err
		}
		manifest.Server.Directory = filepath.ToSlash(relative)
	}
	s.logger.Debug(
		"detected server runtime",
		"minecraft", manifest.Server.Minecraft,
		"product", manifest.Server.Core.Product,
		"core-version", manifest.Server.Core.ProductVersion,
		"directory", manifest.Server.Directory,
	)
	return nil
}

// identificationFailure returns only a cancellation error, which aborts
// initialization. Other identification failures do not abort it and are not
// returned as warnings; their files stay unidentified.
func identificationFailure(errs []error) error {
	for _, err := range errs {
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			return err
		}
	}
	return nil
}

// applyDetectedVersions completes the server declaration from observation:
// a concrete Minecraft version, the default product, and the MCDR runtime.
func (s *Service) applyDetectedVersions(
	manifest *documentpkg.Manifest,
	opts InitOptions,
	observed *workspace.Workspace,
) error {
	if manifest.Server.Minecraft == "" || manifest.Server.Minecraft.IsInvalid() {
		return fmt.Errorf(
			"the detected server has no concrete Minecraft version",
		)
	}
	if manifest.Server.Core.Product == serverpkg.ProductUnknown {
		manifest.Server.Core.Product = serverpkg.ProductMinecraft
		s.logger.Debug(
			"core product defaulted",
			"product", manifest.Server.Core.Product,
		)
	}
	mcdrVersion := opts.MCDRVersion
	if mcdrVersion == "" && observed != nil && observed.Wrappers.MCDR != nil {
		v := observed.Wrappers.MCDR.Version
		if v.IsInvalid() || v.CanInfer() {
			return fmt.Errorf(
				"MCDR detected without an exact app version; specify --mcdr-version",
			)
		}
		mcdrVersion = string(v)
		s.logger.Debug(
			"MCDR version from workspace detection",
			"version", mcdrVersion,
		)
	}
	if mcdrVersion != "" {
		manifest.MCDR = &documentpkg.ManifestMCDR{
			Directory: ".",
			Version:   version.Version(mcdrVersion),
			Packages:  map[string]documentpkg.Requirement{},
		}
	}
	return nil
}

func readExistingLock(root string) (*documentpkg.Lock, error) {
	old, err := documentpkg.LockFile(root).Read()
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return old, err
}

// resolveAdoptedLock resolves the observed declaration. Module requirements
// the installed jars report but the catalogue metadata cannot satisfy become
// warnings instead of a refusal, because adoption records the set the server
// already runs.
func (s *Service) resolveAdoptedLock(
	ctx context.Context,
	root string,
	manifest *documentpkg.Manifest,
	old *documentpkg.Lock,
	opts InitOptions,
	observed *workspace.Workspace,
) (*documentpkg.Lock, error) {
	resolved, resolution, err := s.install.Resolve(
		ctx,
		root,
		manifest,
		old,
		install.Options{
			AcceptBroken: true,
			Trace:        opts.Trace,
			Materialize:  opts.Materialize,
		},
	)
	if observed != nil && resolution != nil {
		observed.Warnings = append(observed.Warnings, resolution.Warnings...)
	}
	if err != nil {
		return nil, err
	}
	return resolved, nil
}

// publishInitialization commits the manifest, lock, and receipt together,
// then registers the workspace and publishes the user configuration.
func (s *Service) publishInitialization(
	ctx context.Context,
	root string,
	manifest *documentpkg.Manifest,
	old *documentpkg.Lock,
	resolved *documentpkg.Lock,
	adoptedPackages []install.AdoptedPackage,
	runtimeObserved *runtimeObservation,
	observed *workspace.Workspace,
	opts InitOptions,
	preconditions install.AdoptionPreconditions,
) (*InitResult, error) {
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

	var runtimeInstallation *discovery.Installation
	if runtimeObserved != nil {
		// Re-discover from disk instead of trusting the in-memory selection
		// that may predate the ambiguity prompt or network identification.
		// Installation membership binds the same observed runtime without
		// turning a member path into a selection key.
		fresh := workspace.Observe(root, discovery.Request{})
		freshInstallation := fresh.Discovery.FindInstallation(runtimeObserved.members)
		if err := runtimeObserved.verify(freshInstallation); err != nil {
			return initResult(observed), err
		}
		runtimeInstallation = freshInstallation
	} else if observed != nil {
		if selection := observed.Runtime(); selection != nil {
			runtimeInstallation = selection.Installation
		}
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
		manifestFile = manifestFile.WithEncoder(
			(*documentpkg.Manifest).PlainBytes,
		)
	}
	var manifestMerge func([]byte) ([]byte, error)
	if !opts.Force && !opts.NoComments {
		manifestMerge = manifest.MergeBytes
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
		Manifest:     manifest,
		Lock:         resolved,
		Runtime:      runtimeInstallation,
		RuntimeFiles: runtimeFiles,
		Packages:     adoptedPackages,
		Logger:       s.logger,
		PublishLock:  publishLock,
		ManifestSnapshot: func() ([]byte, error) {
			return manifestFile.Snapshot(manifest)
		},
		ManifestMerge: manifestMerge,
		Preconditions: preconditions,
	})
	if adoptErr != nil && !errors.As(adoptErr, &committedErr) {
		return initResult(observed), adoptErr
	}

	result := &InitResult{
		Document:      manifest,
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
