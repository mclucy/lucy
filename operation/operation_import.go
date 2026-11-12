package operation

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/artifactstore"
	"github.com/mclucy/lucy/bootstrap"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/importpack"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/receipt"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
)

// Import stage names the command renders. A remote reference and a direct URL
// both fetch an archive, but only a remote reference is a modpack; a local
// source starts directly with writing files.
const (
	importDownloadPackPhase    = "download pack"
	importDownloadArchivePhase = "download archive"
	importFilesPhase           = "files"
)

// importDownloadPhase names the stage that fetches the source, or "" when the
// source is already local.
func importDownloadPhase(source string) string {
	if _, remote, err := ParseRemoteImportSource(source); remote && err == nil {
		return importDownloadPackPhase
	}
	if strings.Contains(source, "://") {
		return importDownloadArchivePhase
	}
	return ""
}

// importProgress adapts the operation trace to importpack's download hooks. A
// nil trace reports nothing.
func importProgress(trace *install.Trace) importpack.Progress {
	if trace == nil {
		return importpack.Progress{}
	}
	return importpack.Progress{
		FileStarted:  trace.ReportFetchStarted,
		FileProgress: trace.ReportFetchProgress,
		FileDone:     trace.ReportFetchDone,
	}
}

// errImportNotInitialized reports an imported tree with no runtime. The tree
// stays extracted in the target, and the operator initializes it once the
// modpack is set up.
var errImportNotInitialized = errors.New(
	"no server detected; the archive was extracted as-is. " +
		"Run `lucy init` once the modpack is set up.",
)

// importSource adopts an external server archive as a new workspace. The
// archive is materialized and initialized in a private staging directory
// beside the target, so the target is untouched until every check that can
// fail has passed.
func (s *Service) importSource(
	ctx context.Context,
	opts ImportOptions,
	network *cache.Client,
) (result *ImportResult, resultErr error) {
	target, err := importTargetPath(opts.Directory)
	if err != nil {
		return nil, err
	}
	if s.registry == nil {
		return nil, errors.New(
			"server registry is unavailable; cannot record the imported workspace",
		)
	}
	lease, err := artifactstore.AcquireOperation()
	if err != nil {
		return nil, err
	}
	defer func() {
		closeErr := lease.Close()
		if closeErr == nil {
			return
		}
		if resultErr == nil && result != nil {
			result.Warnings = append(result.Warnings, fmt.Errorf(
				"release import operation lease: %w", closeErr,
			))
			result.WarningMessages = warningStrings(result.Warnings)
			return
		}
		resultErr = errors.Join(resultErr, closeErr)
	}()

	downloadPhase := importDownloadPhase(opts.Source)
	downloadStarted := time.Now()
	if downloadPhase != "" {
		opts.Trace.ReportPhaseStarted(downloadPhase)
	}
	progress := importProgress(opts.Trace)
	sourcePath, source, releaseSource, err := s.resolveImportSource(
		ctx,
		opts.Source,
		opts.SHA256,
		network,
		progress,
	)
	if err != nil {
		return nil, err
	}
	defer releaseSource()

	plan, err := importpack.Inspect(ctx, sourcePath)
	if err != nil {
		return nil, err
	}
	if downloadPhase != "" {
		opts.Trace.ReportPhase(install.PhaseInfo{
			Name:    downloadPhase,
			Elapsed: time.Since(downloadStarted),
		})
	}

	name := opts.Name
	if name == "" {
		name = filepath.Base(target)
	}

	parent := filepath.Dir(target)
	rollbackParents, err := createImportParents(parent)
	if err != nil {
		return nil, err
	}
	stage := ""
	stageRegistered := false
	published := false
	defer func() {
		if published {
			return
		}
		if stageRegistered {
			resultErr = errors.Join(resultErr, s.unregisterWorkspace(stage))
		}
		if stage != "" {
			resultErr = errors.Join(resultErr, os.RemoveAll(stage))
		}
		rollbackParents()
	}()

	stage, err = os.MkdirTemp(parent, ".lucy-import-*")
	if err != nil {
		return nil, fmt.Errorf("create import staging directory: %w", err)
	}
	if err := os.Chmod(stage, 0o755); err != nil {
		return nil, fmt.Errorf("prepare import staging directory: %w", err)
	}

	filesStarted := time.Now()
	opts.Trace.ReportPhaseStarted(importFilesPhase)
	materialized, err := importpack.Materialize(
		ctx,
		sourcePath,
		stage,
		network,
		progress,
	)
	if err != nil {
		return nil, err
	}
	opts.Trace.ReportPhase(install.PhaseInfo{
		Name:    importFilesPhase,
		Elapsed: time.Since(filesStarted),
		Count:   materialized.ServerFiles,
	})
	if plan.Format == importpack.FormatMrpack {
		// A client modpack marks its client-only files server-required, so the
		// env declaration alone cannot exclude them from a server.
		removed, err := s.removeClientOnlyMods(
			ctx,
			stage,
			materialized.Product,
			materialized.Minecraft,
		)
		if err != nil {
			return nil, err
		}
		materialized.SkippedClient += len(removed)
		materialized.ServerFiles -= len(removed)
		materialized.Skipped = append(materialized.Skipped, removed...)
	}

	var loaderWarnings []error
	force := false
	if plan.Format == importpack.FormatMrpack {
		if err := s.installDeclaredLoader(ctx, stage, name, materialized, opts.Trace); err != nil {
			return nil, err
		}
		force = true
	} else {
		loaderWarnings, err = s.runTreeInstaller(ctx, stage, opts.Trace)
		if err != nil {
			return nil, err
		}
	}

	stageRegistered = true
	initResult, initErr := s.Initialize(ctx, stage, InitOptions{
		Name:          name,
		MCDRVersion:   opts.MCDRVersion,
		Force:         force,
		NoComments:    opts.NoComments,
		Expectation:   opts.Expectation,
		PromptRuntime: opts.PromptRuntime,
		Trace:         opts.Trace,
	})
	uninitialized := errors.Is(initErr, errNoServerDetected)
	if initErr != nil && !uninitialized {
		return nil, initErr
	}
	if uninitialized {
		// applyRuntimeIdentity refuses before registration and document
		// publication, so the stage was never registered.
		stageRegistered = false
		result, err = publishUninitializedImport(
			stage,
			target,
			name,
			source,
			plan.Format,
			materialized,
			loaderWarnings,
			initResult,
		)
		if err != nil {
			return nil, err
		}
		published = true
		return result, nil
	}
	manifest := initResult.Document
	lock := initResult.Lock

	observed := workspace.Observe(stage, discovery.Request{})
	unmanaged, err := unmanagedImportFiles(stage, observed)
	if err != nil {
		return nil, err
	}
	review := importReview(filepath.Join(stage, filepath.FromSlash(manifest.Server.Directory)))

	// Hold the stage's writer lock across publication and registration. The
	// rename moves the lock into the target, so another operation cannot enter
	// between the workspace becoming visible and its registration.
	writer, err := workspacewrite.Lock(stage, s.logger)
	if err != nil {
		return nil, err
	}
	writerHeld := true
	defer func() {
		if writerHeld {
			resultErr = errors.Join(resultErr, writer.Close())
		}
	}()

	publication, err := publishImportStage(stage, target)
	if err != nil {
		return nil, err
	}
	published = true

	result = &ImportResult{
		Workspace:     target,
		Initialized:   true,
		Source:        source,
		Format:        string(plan.Format),
		Name:          manifest.Name,
		Minecraft:     string(manifest.Server.Minecraft),
		Product:       string(manifest.Server.Core.Product),
		CoreVersion:   string(manifest.Server.Core.ProductVersion),
		ServerFiles:   materialized.ServerFiles,
		SkippedClient: materialized.SkippedClient,
		Overrides:     materialized.Overrides,
		Skipped:       materialized.Skipped,
		Unmanaged:     unmanaged,
		Review:        review,
		Manifest:      manifest,
		Lock:          lock,
		Warnings: append(
			append([]error(nil), initResult.Warnings...),
			loaderWarnings...,
		),
		Discovery: initResult.Discovery.RebasePaths(stage, target),
	}
	if lock != nil {
		result.ManagedPackages = len(lock.Packages)
	}

	duplicates, regErr := s.registerWorkspace(target)
	if regErr != nil {
		rollbackErr := publication.rollback()
		closeErr := writer.Close()
		writerHeld = false
		if rollbackErr != nil {
			return result, errors.Join(
				fmt.Errorf("register imported workspace: %w", regErr),
				fmt.Errorf("rollback published workspace: %w", rollbackErr),
			)
		}
		published = false
		return result, errors.Join(
			fmt.Errorf("register imported workspace: %w", regErr),
			closeErr,
		)
	}
	closeErr := writer.Close()
	writerHeld = false
	if closeErr != nil {
		return result, fmt.Errorf("release imported workspace lock: %w", closeErr)
	}
	if err := s.unregisterWorkspace(stage); err != nil {
		result.Warnings = append(result.Warnings, fmt.Errorf(
			"remove import staging registration: %w", err,
		))
	}
	stageRegistered = false
	if err := os.RemoveAll(stage); err != nil {
		result.Warnings = append(result.Warnings, fmt.Errorf(
			"remove import staging directory: %w", err,
		))
	}
	for _, duplicate := range duplicates {
		result.Warnings = append(result.Warnings, fmt.Errorf(
			"server name %q is also registered at %s; --server cannot select an ambiguous name",
			duplicate.Name,
			duplicate.Path,
		))
	}
	result.WarningMessages = warningStrings(result.Warnings)
	return result, nil
}

// publishUninitializedImport publishes the stage of a source that declares no
// loader and ships no usable installer. The tree has no runtime, so it is
// extracted as-is: generated workspace documents are removed, nothing is
// registered, and the result carries a warning to initialize once the modpack
// is set up. The stage is private and unpublished, so no writer lock is
// required.
func publishUninitializedImport(
	stage, target, name, source string,
	format importpack.Format,
	materialized *importpack.Result,
	loaderWarnings []error,
	initResult *InitResult,
) (*ImportResult, error) {
	observed := workspace.Observe(stage, discovery.Request{})
	unmanaged, err := unmanagedImportFilePaths(stage, observed)
	if err != nil {
		return nil, err
	}
	review := importReview(stage)
	if err := removeGeneratedImportArtifacts(stage); err != nil {
		return nil, err
	}
	if _, err := publishImportStage(stage, target); err != nil {
		return nil, err
	}
	result := &ImportResult{
		Workspace:     target,
		Initialized:   false,
		Source:        source,
		Format:        string(format),
		Name:          name,
		ServerFiles:   materialized.ServerFiles,
		SkippedClient: materialized.SkippedClient,
		Overrides:     materialized.Overrides,
		Skipped:       materialized.Skipped,
		Unmanaged:     unmanaged,
		Review:        review,
		Warnings:      append(loaderWarnings, errImportNotInitialized),
	}
	if initResult != nil {
		result.Discovery = initResult.Discovery.RebasePaths(stage, target)
	}
	result.WarningMessages = warningStrings(result.Warnings)
	return result, nil
}

// removeGeneratedImportArtifacts deletes the workspace documents that
// initialization created in the stage. importpack rejects archive entries with
// these names, so only generated artifacts can exist.
func removeGeneratedImportArtifacts(stage string) error {
	for _, name := range []string{
		document.ManifestFilename,
		document.LockFilename,
	} {
		if err := os.Remove(filepath.Join(stage, name)); err != nil &&
			!errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove generated %s: %w", name, err)
		}
	}
	if err := os.RemoveAll(filepath.Join(stage, ".lucy")); err != nil {
		return fmt.Errorf("remove generated workspace state: %w", err)
	}
	return nil
}

// runTreeInstaller runs the loader installer that a source shipping its own
// tree carries. It returns the warnings that leave the extracted files
// uninstalled; any other failure aborts the import.
func (s *Service) runTreeInstaller(
	ctx context.Context,
	stage string,
	trace *install.Trace,
) ([]error, error) {
	installers, err := s.findImportInstallers(ctx, stage)
	if err != nil {
		return nil, err
	}
	switch len(installers) {
	case 0:
		return nil, nil
	case 1:
	default:
		return []error{
			errors.New("multiple loader installers found; none was run"),
		}, nil
	}

	installer := installers[0]
	if !bootstrap.InstallerSupported(installer.Kind) {
		// An unsupported installer is never executed, so importing it must
		// not require a Java runtime.
		return []error{unsupportedInstallerWarning(installer.Kind)}, nil
	}
	advisory := bootstrap.InstallerJavaAdvisory(installer)
	lock := &document.Lock{Server: document.LockedServer{
		Minecraft: installer.Minecraft,
		Java:      document.NewLockedJava(advisory, nil),
	}}
	check, err := s.install.CheckJava(ctx, stage, lock)
	if err != nil {
		return nil, err
	}
	if !check.Satisfied() {
		return nil, fmt.Errorf(
			"run the %s installer: %s",
			installer.Kind,
			strings.Join(check.Problems, "; "),
		)
	}

	const phase = "loader installer"
	started := time.Now()
	trace.ReportPhaseStarted(phase)
	err = bootstrap.RunInstaller(
		ctx,
		importServerRoot(stage),
		installer,
		check.Executable(),
		importProcessOutput(trace),
	)
	trace.ReportPhase(install.PhaseInfo{
		Name:    phase,
		Elapsed: time.Since(started),
	})
	if err != nil {
		if errors.Is(err, bootstrap.ErrInstallerUnsupported) {
			return []error{unsupportedInstallerWarning(installer.Kind)}, nil
		}
		return nil, err
	}
	return nil, nil
}

// unsupportedInstallerWarning reports an installer that was found but cannot
// be executed yet. Its extracted files remain untouched.
func unsupportedInstallerWarning(kind artifact.InstallerKind) error {
	return fmt.Errorf(
		"%s was found but is not supported yet; "+
			"its files were extracted without running it",
		kind,
	)
}

// importServerRoot returns the directory a bundled installer must provision.
// MCDR moves the server into its configured working directory, and
// initialization observes only that directory. A working directory outside the
// staging tree is not trusted, so the stage root answers instead.
func importServerRoot(stage string) string {
	root := workspace.ServerDirectory(stage)
	relative, err := filepath.Rel(stage, root)
	if err != nil || relative == ".." ||
		strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return stage
	}
	return root
}

// findImportInstallers lists the loader installers a tree ships. A pack may
// nest an installer under any directory. A non-archive and an archive without
// installer metadata are not errors, so a failed read is reported at debug
// and does not fail the import.
func (s *Service) findImportInstallers(
	ctx context.Context,
	stage string,
) ([]artifact.Installer, error) {
	var installers []artifact.Installer
	err := filepath.WalkDir(
		stage,
		func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			if !entry.Type().IsRegular() {
				return nil
			}
			installer, found, err := artifact.ReadInstaller(path)
			if err != nil {
				s.logger.Debug(
					"read loader installer",
					"path", path,
					"error", err,
				)
				return nil
			}
			if found {
				installers = append(installers, installer)
			}
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	return installers, nil
}

// importProcessOutput returns the writer that receives installer output. A nil
// trace has none.
func importProcessOutput(trace *install.Trace) io.Writer {
	if trace == nil {
		return nil
	}
	return trace.ProcessOutput
}

// unmanagedImportFilePaths lists every observed package file without asking the
// committed receipt. A tree that is not initialized has no receipt, so all of
// its package files are unmanaged.
func unmanagedImportFilePaths(
	root string,
	observed *workspace.Workspace,
) ([]string, error) {
	paths := make(
		[]string,
		0,
		len(observed.Packages)+len(observed.UnidentifiedPackageFiles),
	)
	for _, pkg := range observed.Packages {
		if pkg.Path != "" {
			paths = append(paths, pkg.Path)
		}
	}
	paths = append(paths, observed.UnidentifiedPackageFiles...)
	relative, err := relativeImportPaths(root, paths)
	if err != nil {
		return nil, err
	}
	slices.Sort(relative)
	return slices.Compact(relative), nil
}

// installDeclaredLoader installs the loader an mrpack declares into the
// private stage. The temporary manifest exists only in the stage; Initialize
// replaces it with the adopted manifest.
func (s *Service) installDeclaredLoader(
	ctx context.Context,
	stage, name string,
	source *importpack.Result,
	trace *install.Trace,
) error {
	manifest := &document.Manifest{
		SchemaVersion: document.ManifestSchemaVersion,
		Name:          name,
		Server: document.ManifestServer{
			Minecraft: version.Version(source.Minecraft),
			Core: document.ManifestCore{
				Product:        source.Product,
				ProductVersion: version.Version(source.CoreVersion),
			},
			Packages: map[catalog.Ecosystem]map[string]document.Requirement{},
		},
	}
	if err := manifest.Normalize(); err != nil {
		return err
	}
	_, err := s.install.Sync(ctx, stage, manifest, nil, install.Options{
		PublishManifest:  true,
		SkipRegistration: true,
		Trace:            trace,
	})
	return err
}

// unregisterWorkspace removes the registry entry for root under the
// workspace's writer lock, as the registry requires.
func (s *Service) unregisterWorkspace(root string) error {
	lock, err := workspacewrite.Lock(root, s.logger)
	if err != nil {
		return err
	}
	return errors.Join(s.registry.Unregister(root), lock.Close())
}

// importTargetPath validates the requested target. The target must be an
// absolute path that is absent or an empty directory.
func importTargetPath(directory string) (string, error) {
	if !filepath.IsAbs(directory) {
		return "", fmt.Errorf("import target must be an absolute directory path: %q", directory)
	}
	target := filepath.Clean(directory)
	info, err := os.Lstat(target)
	if errors.Is(err, os.ErrNotExist) {
		return target, nil
	}
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("import target is not a directory: %s", target)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return "", err
	}
	if len(entries) > 0 {
		return "", fmt.Errorf("import target is not empty: %s", target)
	}
	return target, nil
}

// acquireImportSource returns a local path for the archive, the source as it
// should be reported, and a release function for any temporary download.
func acquireImportSource(
	ctx context.Context,
	source, sha256 string,
	network *cache.Client,
	progress importpack.Progress,
) (string, string, func(), error) {
	if strings.Contains(source, "://") {
		return downloadImportSource(ctx, source, sha256, network, progress)
	}
	local, err := filepath.Abs(source)
	if err != nil {
		return "", "", nil, err
	}
	info, err := os.Stat(local)
	if err != nil {
		return "", "", nil, fmt.Errorf("import source: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", "", nil, fmt.Errorf("import source is not a regular file: %s", local)
	}
	if err := verifyImportSHA256(local, sha256); err != nil {
		return "", "", nil, err
	}
	return local, local, func() {}, nil
}

// downloadImportSource fetches an http(s) archive into a private temporary
// directory. The expected SHA-256 is checked on the downloaded bytes because
// the cache does not verify SHA-256 expectations itself.
func downloadImportSource(
	ctx context.Context,
	source, sha256 string,
	network *cache.Client,
	progress importpack.Progress,
) (string, string, func(), error) {
	parsed, err := url.Parse(source)
	if err != nil {
		return "", "", nil, fmt.Errorf("parse import URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", "", nil, fmt.Errorf(
			"unsupported import source scheme %q: use a local file or an http(s) URL",
			parsed.Scheme,
		)
	}
	if parsed.Host == "" {
		return "", "", nil, fmt.Errorf("import URL has no host: %s", source)
	}
	if network == nil {
		return "", "", nil, errors.New("import URL requires a network client")
	}
	dir, err := os.MkdirTemp("", "lucy-import-source-*")
	if err != nil {
		return "", "", nil, fmt.Errorf("create import download directory: %w", err)
	}
	release := func() { _ = os.RemoveAll(dir) }
	filename := path.Base(parsed.Path)
	if filename == "" || filename == "." || filename == "/" {
		filename = "import-source"
	}
	download, err := network.CachedDownload(ctx, source, dir, cache.DownloadOptions{
		Kind:     cache.KindArtifact,
		Filename: filename,
		FileMode: 0o600,
		MaxBytes: importpack.MaxSourceBytes,
		WrapReader: func(r io.Reader, size int64) io.Reader {
			progress.ReportStarted(filename, size)
			return cache.ProgressReader(r, size, func(read, total int64) {
				progress.ReportProgress(filename, read, total)
			})
		},
	})
	if err != nil {
		release()
		return "", "", nil, err
	}
	file := download.File.Name()
	if err := download.File.Close(); err != nil {
		release()
		return "", "", nil, err
	}
	if err := verifyImportSHA256(file, sha256); err != nil {
		release()
		return "", "", nil, err
	}
	progress.ReportDone(filename, download.CacheHit)
	return file, source, release, nil
}

func verifyImportSHA256(file, expected string) error {
	if expected == "" {
		return nil
	}
	_, err := install.DigestFile(file, map[string]string{
		"sha256": strings.ToLower(expected),
	})
	return err
}

// createImportParents creates parent and returns a rollback that removes only
// the directories it created, deepest first.
func createImportParents(parent string) (func(), error) {
	var missing []string
	for dir := parent; ; {
		info, err := os.Lstat(dir)
		if err == nil {
			if !info.IsDir() {
				return nil, fmt.Errorf("import parent is not a directory: %s", dir)
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		missing = append(missing, dir)
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
		dir = next
	}
	var created []string
	rollback := func() {
		for i := len(created) - 1; i >= 0; i-- {
			_ = os.Remove(created[i])
		}
	}
	for i := len(missing) - 1; i >= 0; i-- {
		if err := os.Mkdir(missing[i], 0o755); err != nil {
			rollback()
			return nil, err
		}
		created = append(created, missing[i])
	}
	return rollback, nil
}

// importPublication records how publishImportStage moved the staged tree into
// the target, so rollback can put it back. The caller holds the stage's writer
// lock, which travels with the moved entries, so a published workspace stays
// exclusively owned until registration finishes.
type importPublication struct {
	stage  string
	target string
	// inPlace records that target was an existing empty directory. Publication
	// moved the stage's top-level entries into it, so rollback moves them back
	// and leaves the directory itself, and its inode, in place.
	inPlace bool
	// moved lists the top-level entry names moved into target, in move order.
	moved []string
}

func (p *importPublication) rollback() error {
	if p == nil {
		return nil
	}
	if !p.inPlace {
		return os.Rename(p.target, p.stage)
	}
	var errs []error
	for i := len(p.moved) - 1; i >= 0; i-- {
		name := p.moved[i]
		if err := os.Rename(filepath.Join(p.target, name), filepath.Join(p.stage, name)); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// publishImportStage publishes a staged tree into target. An absent target is
// created by renaming the stage into place. An existing empty directory is kept
// and receives the stage's top-level entries, so its inode and the invoking
// shell's working directory remain valid. The target must be absent or an empty
// directory, rechecked after staging. The caller must hold the stage's writer
// lock.
func publishImportStage(stage, target string) (*importPublication, error) {
	publication := &importPublication{stage: stage, target: target}
	info, err := os.Lstat(target)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err := os.Rename(stage, target); err != nil {
			return nil, fmt.Errorf("publish import stage: %w", err)
		}
		return publication, nil
	case err != nil:
		return nil, fmt.Errorf("inspect import target: %w", err)
	case !info.IsDir():
		return nil, fmt.Errorf("import target is not a directory: %s", target)
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return nil, fmt.Errorf("inspect import target: %w", err)
	}
	if len(entries) > 0 {
		return nil, fmt.Errorf("import target is not empty: %s", target)
	}
	staged, err := os.ReadDir(stage)
	if err != nil {
		return nil, fmt.Errorf("read import stage: %w", err)
	}
	publication.inPlace = true
	for _, entry := range staged {
		name := entry.Name()
		dst := filepath.Join(target, name)
		if _, err := os.Lstat(dst); err == nil {
			return nil, errors.Join(fmt.Errorf("import target gained entry: %s", dst), publication.rollback())
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, errors.Join(fmt.Errorf("inspect import target: %w", err), publication.rollback())
		}
		if err := os.Rename(filepath.Join(stage, name), dst); err != nil {
			return nil, errors.Join(fmt.Errorf("publish import stage: %w", err), publication.rollback())
		}
		publication.moved = append(publication.moved, name)
	}
	return publication, nil
}

// removeClientOnlyMods deletes mod jars a dedicated server cannot use. The
// provider's project labels decide first; descriptors answer files no provider
// identifies. A client-required project that is merely server-optional is a
// candidate, not a certainty: a server package may depend on it through its
// descriptor or its provider release, so candidates stay when a kept package
// names one of their modules or projects.
func (s *Service) removeClientOnlyMods(
	ctx context.Context,
	stage string,
	product serverpkg.Product,
	minecraft string,
) ([]string, error) {
	modsDir := filepath.Join(stage, "mods")
	entries, err := os.ReadDir(modsDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read imported mod directory: %w", err)
	}
	ecosystem := catalog.Ecosystem(product)
	mappers, mapperErr := s.packageMappers()
	if mapperErr != nil {
		s.logger.Debug(
			"package mappers unavailable during import",
			"error", mapperErr,
		)
	}

	var paths []string
	for _, entry := range entries {
		if entry.IsDir() ||
			!strings.EqualFold(filepath.Ext(entry.Name()), ".jar") {
			continue
		}
		paths = append(paths, filepath.Join(modsDir, entry.Name()))
	}
	files := make([]importModFile, len(paths))
	limit := make(chan struct{}, packageProbeLimit)
	var wg sync.WaitGroup
	for i, path := range paths {
		wg.Go(func() {
			limit <- struct{}{}
			defer func() { <-limit }()
			files[i] = s.classifyImportMod(
				ctx,
				path,
				ecosystem,
				minecraft,
				mappers,
			)
		})
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	keepRequiredMods(files)

	var removed []string
	for _, file := range files {
		if file.kept {
			continue
		}
		if err := os.Remove(file.path); err != nil {
			return removed, fmt.Errorf(
				"remove client-only mod %s: %w",
				file.path,
				err,
			)
		}
		relative, err := filepath.Rel(stage, file.path)
		if err != nil {
			return removed, fmt.Errorf(
				"relativize client-only mod %s: %w",
				file.path,
				err,
			)
		}
		s.logger.Debug(
			"removed client-only mod",
			"path", filepath.ToSlash(relative),
			"reason", file.reason,
		)
		removed = append(removed, filepath.ToSlash(relative))
	}
	return removed, nil
}

// classifyImportMod reads one archive and decides whether a dedicated server
// can use it. Classification runs concurrently across files, so the helper
// reads no shared mutable state.
func (s *Service) classifyImportMod(
	ctx context.Context,
	path string,
	ecosystem catalog.Ecosystem,
	minecraft string,
	mappers []packageMapper,
) importModFile {
	modules := importModules(ctx, path, ecosystem, minecraft)
	file := importModFile{
		path:    path,
		modules: modules,
		kept:    true,
	}
	release, matched := s.identifiedRelease(ctx, path, mappers)
	if matched {
		file.project = projectKey(
			release.Candidate.Provider,
			release.Candidate.ProjectID,
		)
		file.dependencies = release.Candidate.Dependencies
		if release.ServerSide != "" || release.ClientSide != "" {
			switch {
			case release.ServerSide == upstream.ServerSideUnsupported:
				file.reason = "project does not support a dedicated server"
				file.removable, file.kept = true, false
			case release.ClientOnly():
				file.reason = "project declares client-only support"
				file.kept = false
			}
			return file
		}
	}
	if reason, clientOnly := clientOnlyDescriptor(
		ctx,
		path,
		ecosystem,
		minecraft,
	); clientOnly {
		file.reason = reason
		file.removable, file.kept = true, false
	} else if project, found := s.catalogProjectSide(
		ctx,
		path,
		mappers,
	); found {
		switch {
		case project.ServerSide == upstream.ServerSideUnsupported:
			file.reason = "project does not support a dedicated server"
			file.removable, file.kept = true, false
		case project.IsClientOnly():
			file.reason = "project declares client-only support"
			file.kept = false
		}
	}
	return file
}

// importModFile is one imported mod archive and its classification.
type importModFile struct {
	path string
	// project is the provider-qualified project the file was published as.
	project string
	// dependencies lists the project relationships the release declares.
	dependencies []upstream.ProjectDependency
	modules      []catalog.DescriptorModule
	reason       string
	kept         bool
	removable    bool
}

// keepRequiredMods keeps candidates a server package depends on, whether the
// dependency comes from a descriptor module or the provider's release
// metadata. A promoted candidate can itself require another candidate, so the
// pass repeats until nothing changes.
func keepRequiredMods(files []importModFile) {
	for {
		requiredModules := requiredModuleIDs(files)
		requiredProjectKeys := requiredProjectIDs(files)
		changed := false
		for i := range files {
			if files[i].kept || files[i].removable {
				continue
			}
			if modulesProvide(files[i].modules, requiredModules) ||
				(files[i].project != "" && requiredProjectKeys[files[i].project]) {
				files[i].kept = true
				changed = true
			}
		}
		if !changed {
			return
		}
	}
}

// requiredModuleIDs lists every dependency a kept archive declares.
func requiredModuleIDs(files []importModFile) map[string]bool {
	required := make(map[string]bool)
	for _, file := range files {
		if !file.kept {
			continue
		}
		for _, module := range file.modules {
			for _, dependency := range module.Dependencies {
				addDependencyIDs(required, dependency)
			}
		}
	}
	return required
}

// requiredProjectIDs lists the provider projects that kept files require.
func requiredProjectIDs(files []importModFile) map[string]bool {
	required := make(map[string]bool)
	for _, file := range files {
		if !file.kept {
			continue
		}
		for _, dependency := range file.dependencies {
			if dependency.Kind != catalog.DependencyKindRequired {
				continue
			}
			if key := projectKey(dependency.Provider, dependency.ProjectID); key != "" {
				required[key] = true
			}
		}
	}
	return required
}

func addDependencyIDs(ids map[string]bool, dependency catalog.DescriptorDependency) {
	switch dependency.Kind {
	case catalog.DependencyKindIncompatible, catalog.DependencyKindConflict:
		// A negative relation keeps the candidate away; it does not require it.
		return
	}
	if dependency.ID != "" {
		ids[dependency.ID] = true
	}
	for _, alternative := range dependency.AnyOf {
		addDependencyIDs(ids, alternative)
	}
	for _, condition := range dependency.AllOf {
		addDependencyIDs(ids, condition)
	}
}

// modulesProvide reports whether any module or provided identity satisfies one
// of the required IDs.
func modulesProvide(
	modules []catalog.DescriptorModule,
	required map[string]bool,
) bool {
	for _, module := range modules {
		if required[module.ID] {
			return true
		}
		if module.Group != "" && required[module.Group+":"+module.ID] {
			return true
		}
		for _, provided := range module.Provides {
			if required[provided] {
				return true
			}
		}
	}
	return false
}

// descriptorLoaders lists the loaders whose descriptor the file carries,
// starting with the server loader. A file with no readable descriptor answers
// only the server loader.
func descriptorLoaders(path string, loader catalog.Ecosystem) []catalog.Ecosystem {
	loaders := []catalog.Ecosystem{loader}
	infos, err := artifact.ReadInfo(path)
	if err != nil {
		return loaders
	}
	for _, info := range infos {
		ecosystem := info.Ref.Ecosystem
		if ecosystem == catalog.EcosystemUnspecified ||
			slices.Contains(loaders, ecosystem) {
			continue
		}
		loaders = append(loaders, ecosystem)
	}
	return loaders
}

// importModules reads the modules that describe a file. The server loader's
// descriptor answers first; a file it has no descriptor for (a Fabric jar
// bridged by Connector) contributes the modules of the descriptor it carries,
// so the dependencies that keep server-optional libraries stay visible.
func importModules(
	ctx context.Context,
	path string,
	loader catalog.Ecosystem,
	minecraft string,
) []catalog.DescriptorModule {
	for _, candidate := range descriptorLoaders(path, loader) {
		modules, err := artifact.ReadModulesFor(ctx, path, candidate, minecraft)
		if err == nil && len(modules) > 0 {
			return modules
		}
	}
	return nil
}

// clientOnlyDescriptor reports which descriptor makes the file client-only.
// The server loader is tried first because its view decides whether the file
// can load at all. A file whose descriptor belongs to another ecosystem (a
// Fabric jar bridged by Connector) is checked with that descriptor's loader.
func clientOnlyDescriptor(
	ctx context.Context,
	path string,
	loader catalog.Ecosystem,
	minecraft string,
) (string, bool) {
	for _, candidate := range descriptorLoaders(path, loader) {
		clientOnly, known, err := artifact.ReadClientOnly(
			ctx,
			path,
			candidate,
			minecraft,
		)
		if err != nil || !known {
			continue
		}
		if clientOnly {
			return "client-only " + string(candidate), true
		}
		if candidate == loader {
			// The server loader accepts the file, so another loader's
			// client-only descriptor cannot remove it.
			return "", false
		}
	}
	return "", false
}

// catalogProjectSide asks a provider that publishes side labels for the
// file's project. The descriptor title and project name select the project; a
// provider title must agree with the descriptor title, so a same-named but
// different project cannot classify the file.
func (s *Service) catalogProjectSide(
	ctx context.Context,
	path string,
	mappers []packageMapper,
) (upstream.ProjectServerSide, bool) {
	infos, err := artifact.ReadInfo(path)
	if err != nil {
		return upstream.ProjectServerSide{}, false
	}
	for _, info := range infos {
		names := projectNameCandidates(info)
		if len(names) == 0 {
			continue
		}
		for _, mapper := range mappers {
			resolver, ok := mapper.release.(upstream.ServerSideResolver)
			if !ok {
				continue
			}
			for _, candidate := range names {
				project, found, err := resolver.ServerSideByName(
					ctx,
					candidate,
				)
				if err != nil {
					s.logger.Debug(
						"project side lookup failed",
						"name", candidate,
						"provider", mapper.provider,
						"error", err,
					)
					continue
				}
				if !found {
					continue
				}
				if info.Metadata.Title == "" ||
					!sameProjectTitle(info.Metadata.Title, project.Title) {
					break
				}
				return project, true
			}
		}
	}
	return upstream.ProjectServerSide{}, false
}

// projectNameCandidates lists the names a provider may use for the project:
// the descriptor title as a slug, then the module name.
func projectNameCandidates(info artifact.PackageInfo) []string {
	var candidates []string
	add := func(name string) {
		if name == "" || slices.Contains(candidates, name) {
			return
		}
		candidates = append(candidates, name)
	}
	add(slugify(info.Metadata.Title))
	add(string(info.Ref.Name))
	return candidates
}

// slugify lowercases a project title and joins its words with hyphens.
func slugify(raw string) string {
	var b strings.Builder
	pending := false
	for _, r := range strings.ToLower(raw) {
		alphanumeric := (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
		if !alphanumeric {
			pending = true
			continue
		}
		if pending && b.Len() > 0 {
			b.WriteByte('-')
		}
		pending = false
		b.WriteRune(r)
	}
	return b.String()
}

// sameProjectTitle reports whether two titles name the same project. An empty
// title on either side gives the slug lookup the benefit of the doubt.
func sameProjectTitle(left, right string) bool {
	a, b := slugify(left), slugify(right)
	if a == "" || b == "" {
		return true
	}
	return a == b
}

// identifiedRelease asks the release mappers whether a provider identifies the
// file's hash, and returns that provider's record: side labels, project
// identity, and declared project dependencies. A miss for every mapper reports
// false, which sends the caller to descriptors.
func (s *Service) identifiedRelease(
	ctx context.Context,
	path string,
	mappers []packageMapper,
) (*upstream.Release, bool) {
	for _, mapper := range mappers {
		release, matched, err := mapper.release.ReleaseByHash(
			ctx,
			artifact.File{Path: path},
		)
		if err != nil {
			s.logger.Debug(
				"artifact hash lookup failed",
				"path", path,
				"provider", mapper.provider,
				"error", err,
			)
			continue
		}
		if !matched {
			continue
		}
		if release.Candidate.Provider == "" {
			release.Candidate.Provider = mapper.provider
		}
		return &release, true
	}
	return nil, false
}

// projectKey identifies one provider project across the files of an import.
// An empty provider or project has no identity to match.
func projectKey(provider catalog.Provider, projectID string) string {
	if provider == "" || projectID == "" {
		return ""
	}
	return string(provider) + ":" + projectID
}

// relativeImportPaths reports files relative to root, slash-separated, so no
// staging path leaks into the result.
func relativeImportPaths(root string, files []string) ([]string, error) {
	var relative []string
	for _, file := range files {
		rel := file
		if filepath.IsAbs(file) {
			var err error
			rel, err = filepath.Rel(root, file)
			if err != nil {
				return nil, fmt.Errorf("relativize %s: %w", file, err)
			}
		}
		relative = append(relative, filepath.ToSlash(rel))
	}
	return relative, nil
}

// unmanagedImportFiles lists observed package files that the committed receipt
// does not own. A readable descriptor alone does not make a file managed:
// adoption leaves packages without a matching release unmanaged.
func unmanagedImportFiles(
	root string,
	observed *workspace.Workspace,
) ([]string, error) {
	committed, err := receipt.Read(root)
	if err != nil {
		return nil, fmt.Errorf("read imported installation receipt: %w", err)
	}
	owned := make(map[string]struct{}, len(committed.Entries))
	for _, entry := range committed.Entries {
		owned[entry.Path] = struct{}{}
	}
	paths := make(
		[]string,
		0,
		len(observed.Packages)+len(observed.UnidentifiedPackageFiles),
	)
	for _, pkg := range observed.Packages {
		if pkg.Path != "" {
			paths = append(paths, pkg.Path)
		}
	}
	paths = append(paths, observed.UnidentifiedPackageFiles...)
	relative, err := relativeImportPaths(root, paths)
	if err != nil {
		return nil, err
	}
	unmanaged := make([]string, 0, len(relative))
	for _, file := range relative {
		if _, ok := owned[file]; ok {
			continue
		}
		unmanaged = append(unmanaged, file)
	}
	slices.Sort(unmanaged)
	return slices.Compact(unmanaged), nil
}

// importReview reports server settings and grants that the operator should
// check before the imported server is run. A metadata file that cannot be read
// within importMetadataMaxBytes is reported as unreadable instead of failing
// the import.
func importReview(serverDir string) []string {
	var review []string
	props, err := importPropertyValues(
		filepath.Join(serverDir, "server.properties"),
		"online-mode",
		"enable-rcon",
	)
	if err != nil {
		review = append(review, "server.properties: unreadable metadata")
	}
	if strings.EqualFold(props["online-mode"], "false") {
		review = append(review, "server.properties: online-mode=false; players are not authenticated with Mojang")
	}
	if strings.EqualFold(props["enable-rcon"], "true") {
		review = append(review, "server.properties: enable-rcon=true; remote console access is enabled")
	}
	eula, err := importPropertyValues(filepath.Join(serverDir, "eula.txt"), "eula")
	if err != nil {
		review = append(review, "eula.txt: unreadable metadata")
	}
	if strings.EqualFold(eula["eula"], "true") {
		review = append(review, "eula.txt: eula=true; the Minecraft EULA was accepted in the source")
	}
	ops, err := importOperatorCount(filepath.Join(serverDir, "ops.json"))
	if err != nil {
		review = append(review, "ops.json: unreadable operator list")
	} else if ops > 0 {
		review = append(review, fmt.Sprintf("ops.json: %d operator(s) granted", ops))
	}
	return review
}

// errImportMetadataTooLarge reports a metadata file larger than
// importMetadataMaxBytes.
var errImportMetadataTooLarge = errors.New("import metadata exceeds size limit")

// importPropertyValues returns the last value of each requested key in a
// key=value file. Keys absent from the file are absent from the result, and a
// missing file is not an error. The file is streamed line by line through a
// reader capped at importMetadataMaxBytes, so a larger file fails with
// errImportMetadataTooLarge without its contents being loaded.
func importPropertyValues(file string, keys ...string) (map[string]string, error) {
	values := make(map[string]string, len(keys))
	f, err := os.Open(file)
	if errors.Is(err, os.ErrNotExist) {
		return values, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() > importMetadataMaxBytes {
		return nil, importMetadataTooLarge(file)
	}
	// One byte past the cap lets a file that grows after Stat be detected
	// below instead of being read to its end.
	limited := &io.LimitedReader{R: f, N: importMetadataMaxBytes + 1}
	scanner := bufio.NewScanner(limited)
	scanner.Buffer(make([]byte, 0, 4096), importMetadataMaxBytes+1)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || line[0] == '#' || line[0] == '!' {
			continue
		}
		k, v, ok := bytes.Cut(line, []byte("="))
		if !ok {
			continue
		}
		name := bytes.TrimSpace(k)
		for _, key := range keys {
			if string(name) == key {
				values[key] = string(bytes.TrimSpace(v))
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if limited.N <= 0 {
		return nil, importMetadataTooLarge(file)
	}
	return values, nil
}

// importMetadataTooLarge describes a metadata file over importMetadataMaxBytes.
func importMetadataTooLarge(file string) error {
	return fmt.Errorf("%s exceeds %d bytes: %w", file, importMetadataMaxBytes, errImportMetadataTooLarge)
}

// importMetadataMaxBytes caps the review metadata Lucy reads from a server
// directory. Operator lists are small; a larger file is reported as unreadable
// instead of being loaded.
const importMetadataMaxBytes = 1 << 20

// importOperatorCount counts entries in ops.json. A missing file has none. A
// file larger than importMetadataMaxBytes is an error.
func importOperatorCount(file string) (int, error) {
	f, err := os.Open(file)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, importMetadataMaxBytes+1))
	if err != nil {
		return 0, err
	}
	if len(data) > importMetadataMaxBytes {
		return 0, importMetadataTooLarge(file)
	}
	var ops []any
	if err := json.Unmarshal(data, &ops); err != nil {
		return 0, err
	}
	return len(ops), nil
}
