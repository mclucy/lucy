package operation

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/artifactstore"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/importpack"
	"github.com/mclucy/lucy/internal/workspacewrite"
	"github.com/mclucy/lucy/receipt"
	"github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
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

	sourcePath, source, releaseSource, err := s.resolveImportSource(
		ctx,
		opts.Source,
		opts.SHA256,
		network,
	)
	if err != nil {
		return nil, err
	}
	defer releaseSource()

	plan, err := importpack.Inspect(ctx, sourcePath)
	if err != nil {
		return nil, err
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

	materialized, err := importpack.Materialize(ctx, sourcePath, stage, network)
	if err != nil {
		return nil, err
	}

	force := false
	if plan.Format == importpack.FormatMrpack {
		if err := s.installDeclaredLoader(ctx, stage, name, materialized, opts.Trace); err != nil {
			return nil, err
		}
		force = true
	}

	stageRegistered = true
	initResult, err := s.Initialize(ctx, stage, InitOptions{
		Name:          name,
		MCDRVersion:   opts.MCDRVersion,
		Force:         force,
		NoComments:    opts.NoComments,
		Expectation:   opts.Expectation,
		PromptRuntime: opts.PromptRuntime,
		Trace:         opts.Trace,
	})
	if err != nil {
		return nil, err
	}
	manifest := initResult.Document
	lock := initResult.Lock

	observed := workspace.Observe(stage, discovery.Request{})
	unmanaged, err := unmanagedImportFiles(stage, observed)
	if err != nil {
		return nil, err
	}
	review, err := importReview(filepath.Join(stage, filepath.FromSlash(manifest.Server.Directory)))
	if err != nil {
		return nil, err
	}

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
		Warnings:      append([]error(nil), initResult.Warnings...),
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
) (string, string, func(), error) {
	if strings.Contains(source, "://") {
		return downloadImportSource(ctx, source, sha256, network)
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
// check before the imported server is run.
func importReview(serverDir string) ([]string, error) {
	var review []string
	onlineMode, found, err := importPropertyValue(
		filepath.Join(serverDir, "server.properties"),
		"online-mode",
	)
	if err != nil {
		return nil, err
	}
	if found && strings.EqualFold(onlineMode, "false") {
		review = append(review, "server.properties: online-mode=false; players are not authenticated with Mojang")
	}
	rcon, found, err := importPropertyValue(
		filepath.Join(serverDir, "server.properties"),
		"enable-rcon",
	)
	if err != nil {
		return nil, err
	}
	if found && strings.EqualFold(rcon, "true") {
		review = append(review, "server.properties: enable-rcon=true; remote console access is enabled")
	}
	eula, found, err := importPropertyValue(filepath.Join(serverDir, "eula.txt"), "eula")
	if err != nil {
		return nil, err
	}
	if found && strings.EqualFold(eula, "true") {
		review = append(review, "eula.txt: eula=true; the Minecraft EULA was accepted in the source")
	}
	ops, err := importOperatorCount(filepath.Join(serverDir, "ops.json"))
	if err != nil {
		review = append(review, "ops.json: unreadable operator list")
	} else if ops > 0 {
		review = append(review, fmt.Sprintf("ops.json: %d operator(s) granted", ops))
	}
	return review, nil
}

// importPropertyValue returns the last value for key in a key=value file. A
// missing file is not an error.
func importPropertyValue(file, key string) (string, bool, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	var value string
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if ok && strings.TrimSpace(k) == key {
			value = strings.TrimSpace(v)
			found = true
		}
	}
	return value, found, nil
}

// importOperatorCount counts entries in ops.json. A missing file has none.
func importOperatorCount(file string) (int, error) {
	data, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var ops []any
	if err := json.Unmarshal(data, &ops); err != nil {
		return 0, err
	}
	return len(ops), nil
}
