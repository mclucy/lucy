package operation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/artifactstore"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/config"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/resolve"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
	"github.com/mclucy/lucy/workspace/identify"
)

// Initialize adopts the server already present in root: it observes the
// runtime and packages, resolves the lock, and publishes the manifest, lock,
// and installation receipt as one workspace transaction.
func (s *Service) Initialize(ctx context.Context, root string, opts InitOptions) (result *InitResult, resultErr error) {
	return s.initialize(ctx, root, opts, new(artifact.Analyses), nil)
}

func (s *Service) initialize(ctx context.Context, root string, opts InitOptions, analyses *artifact.Analyses, known map[string]upstream.ArtifactReleaseResolution) (result *InitResult, resultErr error) {
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
	documents, err := install.CaptureWorkspaceDocuments(root)
	if err != nil {
		return nil, err
	}

	manifest, err := initialManifest(root, opts)
	if err != nil {
		return nil, err
	}
	// Runtime discovery, selection, and identity form one reported phase.
	// Any error or prompt cancellation returns before completion, so the
	// trace keeps the phase active instead of inventing an elapsed time.
	opts.Trace.ReportPhaseStarted("identification")
	identificationStarted := time.Now()
	observed := workspace.ObserveWithAnalyses(root, discovery.Request{Expectation: opts.Expectation}, analyses)
	if err := promptRuntime(opts, observed); err != nil {
		return initResult(observed), err
	}
	// Adoption checks the bytes and mode captured before resolution, so a
	// replacement does not inherit the ownership of an observed file.
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
	opts.Trace.ReportPhase(install.PhaseInfo{
		Name:    "identification",
		Elapsed: time.Since(identificationStarted),
	})

	adoptionStarted := time.Now()
	opts.Trace.ReportPhaseStarted("adoption")
	if known == nil && len(observed.UnidentifiedPackageFiles) > 0 {
		known = make(map[string]upstream.ArtifactReleaseResolution, len(observed.UnidentifiedPackageFiles))
	}
	identificationErrors := identify.Packages(
		ctx,
		observed,
		s.source.ResolveArtifactsByHash,
		known,
	)
	if err := ctx.Err(); err != nil {
		return initResult(observed), err
	}
	fatalIdentification, identificationWarnings := identificationOutcome(
		identificationErrors,
	)
	if fatalIdentification != nil {
		return initResult(observed), fatalIdentification
	}
	// Keep per-attempt details at debug level. Adoption reports incomplete
	// lookups when they prevent identification.
	for _, warning := range identificationWarnings {
		s.logger.Debug("package identification failed", "error", warning)
	}
	if err := s.applyDetectedVersions(manifest, opts, observed); err != nil {
		return initResult(observed), err
	}
	if err := manifest.Normalize(); err != nil {
		return initResult(observed), err
	}
	adoptedPackages, adoptedReleases, reportedMissing, inspected, err := s.adoptInstalledPackages(
		ctx,
		root,
		manifest,
		observed,
		known,
	)
	if err != nil {
		return initResult(observed), err
	}
	opts.Trace.ReportPhase(install.PhaseInfo{
		Name:    "adoption",
		Elapsed: time.Since(adoptionStarted),
		Count:   len(adoptedPackages),
		Total:   inspected,
	})
	old, err := documents.DecodeLock()
	if err != nil {
		return initResult(observed), err
	}
	resolved, err := s.resolveAdoptedLock(
		ctx,
		root,
		manifest,
		old,
		opts,
		observed,
		runtimeObserved,
		adoptedReleases,
		reportedMissing,
	)
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
		documents,
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

// errNoServerDetected reports an adopted tree that holds no runtime. Import
// extracts such a tree instead of failing.
var errNoServerDetected = errors.New("no server detected")

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
			"%w; use `lucy create` to generate a new server",
			errNoServerDetected,
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

// identificationOutcome separates a cancellation, which aborts
// initialization, from identification failures. A failure leaves the file
// unidentified and adoption reports it when no provider identifies it.
func identificationOutcome(errs []error) (error, []error) {
	var fatal error
	warnings := make([]error, 0, len(errs))
	for _, err := range errs {
		if errors.Is(err, context.Canceled) ||
			errors.Is(err, context.DeadlineExceeded) {
			fatal = err
			continue
		}
		warnings = append(warnings, err)
	}
	return fatal, warnings
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
	mcdrVersion := version.Version(opts.MCDRVersion)
	detectedMCDR := observed != nil && observed.Wrappers.MCDR != nil
	if mcdrVersion == "" && detectedMCDR {
		observedVersion := observed.Wrappers.MCDR.Version
		if observedVersion.IsInvalid() || observedVersion.CanInfer() {
			// The lock needs a concrete version, so resolution records the
			// latest published release. The warning keeps this substitution
			// visible to the user.
			observed.Warnings = append(observed.Warnings, fmt.Errorf("failed to detect the system's MCDReforged version"))
		} else {
			mcdrVersion = observedVersion
			s.logger.Debug(
				"MCDR version from workspace detection",
				"version", mcdrVersion,
			)
		}
	}
	if mcdrVersion != "" || detectedMCDR {
		if mcdrVersion == "" {
			mcdrVersion = version.Any
		}
		manifest.MCDR = &documentpkg.ManifestMCDR{
			Directory: ".",
			Version:   mcdrVersion,
			Packages:  map[string]documentpkg.Requirement{},
		}
	}
	return nil
}

// resolveAdoptedLock resolves the observed declaration. Module requirements
// the installed jars report but the catalogue metadata cannot satisfy become
// warnings instead of a refusal, because adoption records the set the server
// already runs. A missing module the adoption report already named with its
// files does not warn again. The selected runtime's observed files are offered
// to resolution, so verified local bytes are imported instead of downloaded.
func (s *Service) resolveAdoptedLock(
	ctx context.Context,
	root string,
	manifest *documentpkg.Manifest,
	old *documentpkg.Lock,
	opts InitOptions,
	observed *workspace.Workspace,
	runtimeObserved *runtimeObservation,
	releases []install.AdoptedRelease,
	reportedMissing map[string]struct{},
) (*documentpkg.Lock, error) {
	var localRuntime []install.AdoptedRuntimeFile
	if runtimeObserved != nil {
		localRuntime = runtimeObserved.files
	}
	resolved, resolution, err := s.install.Resolve(
		ctx,
		root,
		manifest,
		old,
		install.Options{
			AcceptBroken: true,
			Trace:        opts.Trace,
			Materialize:  opts.Materialize,
			Adopted:      releases,
			LocalRuntime: localRuntime,
		},
	)
	if observed != nil && resolution != nil {
		warnings := resolution.Warnings
		if len(reportedMissing) > 0 {
			warnings = dropReportedMissingModules(warnings, reportedMissing)
		}
		observed.Warnings = append(observed.Warnings, warnings...)
	}
	if err != nil {
		return nil, err
	}
	return resolved, nil
}

// dropReportedMissingModules drops resolution warnings for absent modules the
// adoption report already named. Every other warning stays.
func dropReportedMissingModules(
	warnings []error,
	reported map[string]struct{},
) []error {
	kept := warnings[:0]
	for _, warning := range warnings {
		var missing *resolve.MissingModuleError
		if errors.As(warning, &missing) {
			if _, ok := reported[missing.Required]; ok {
				continue
			}
		}
		kept = append(kept, warning)
	}
	return kept
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
	documents install.WorkspaceDocuments,
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

	const imagePhase = "runtime image"
	opts.Trace.ReportPhaseStarted(imagePhase)
	imageStarted := time.Now()
	image, err := s.install.RuntimeImage(ctx, root, resolved, install.Options{
		Trace:       opts.Trace,
		Materialize: opts.Materialize,
	})
	if errors.Is(err, install.ErrRuntimeImageUnavailable) {
		observed.Warnings = append(observed.Warnings, fmt.Errorf(
			"%w; the next lucy install verifies and owns the remaining runtime files",
			err,
		))
	} else if err != nil {
		return initResult(observed), err
	}
	opts.Trace.ReportPhase(install.PhaseInfo{
		Name:    imagePhase,
		Elapsed: time.Since(imageStarted),
		Count:   len(image),
	})

	var runtimeFiles []install.AdoptedRuntimeFile
	if runtimeObserved != nil {
		// Re-discover from disk instead of trusting the in-memory selection
		// that may predate the ambiguity prompt or network identification.
		// Installation membership binds the same observed runtime without
		// turning a member path into a selection key.
		fresh := workspace.ObserveRuntime(root, discovery.Request{})
		freshInstallation := fresh.Discovery.FindInstallation(runtimeObserved.members)
		if err := runtimeObserved.verify(freshInstallation); err != nil {
			return initResult(observed), err
		}
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
	adoptWarnings, adoptErr := install.AdoptExistingWorkspace(root, install.AdoptOptions{
		Manifest:     manifest,
		Lock:         resolved,
		Image:        image,
		RuntimeFiles: runtimeFiles,
		Packages:     adoptedPackages,
		Logger:       s.logger,
		PublishLock:  publishLock,
		ManifestSnapshot: func() ([]byte, error) {
			return manifestFile.Snapshot(manifest)
		},
		ManifestMerge: manifestMerge,
		Preconditions: documents,
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
	result.Warnings = append(result.Warnings, adoptWarnings...)
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
