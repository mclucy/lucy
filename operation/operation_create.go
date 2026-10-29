package operation

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/catalog"
	documentpkg "github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/filetrack"
	"github.com/mclucy/lucy/install"
	"github.com/mclucy/lucy/internal/workspacewrite"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
)

// CreateKind identifies the machine-readable document this operation emits.
const CreateKind = "create"

// CreateOptions describes a new workspace declaration and the installation
// that may follow it. Directory names the workspace root; the server occupies
// that directory directly.
type CreateOptions struct {
	Directory      string
	Name           string
	Minecraft      string
	Product        string
	ProductVersion string
	MCDRVersion    string
	Force          bool
	Install        bool
	InstallOptions install.Options
	Trace          *install.Trace
}

// CreateTarget is the result of observing a directory before creation. It
// writes nothing.
type CreateTarget struct {
	Path             string
	Exists           bool
	NonEmpty         bool
	ManifestExists   bool
	DeclaredPackages int
	Entries          []string
}

// CreateResult projects the resolution and optional installation into one
// document. The embedded result carries the shared operation facts; the
// surrounding fields describe the workspace itself.
type CreateResult struct {
	install.Result
	Workspace        string                `json:"workspace"`
	NonEmpty         bool                  `json:"non_empty"`
	ReplacedManifest bool                  `json:"replaced_manifest,omitempty"`
	Installed        bool                  `json:"installed"`
	Manifest         *documentpkg.Manifest `json:"-"`
	Lock             *documentpkg.Lock     `json:"-"`
}

// CreateResolveError reports a failure before any workspace document was
// written. Workspace may already exist because resolution needs its download
// store.
type CreateResolveError struct {
	Workspace string
	Product   serverpkg.Product
	Minecraft version.Version
	Selector  version.Version
	Err       error
}

func (e *CreateResolveError) Error() string { return e.Err.Error() }

func (e *CreateResolveError) Unwrap() error { return e.Err }

// CreatePublishError reports a failure while publishing the manifest and lock.
type CreatePublishError struct {
	Workspace       string
	ManifestWritten bool
	Err             error
}

func (e *CreatePublishError) Error() string { return e.Err.Error() }

func (e *CreatePublishError) Unwrap() error { return e.Err }

// CreateInstallError reports a failure after the workspace was published. The
// manifest and lock stay in place so the installation can be retried.
type CreateInstallError struct {
	Workspace string
	Err       error
}

func (e *CreateInstallError) Error() string { return e.Err.Error() }

func (e *CreateInstallError) Unwrap() error { return e.Err }

// InspectCreateTarget observes path without creating it. Entries carries at
// most five names so a later warning can name what is already there.
func InspectCreateTarget(path string) (CreateTarget, error) {
	absolute, err := createAbsolute(path)
	if err != nil {
		return CreateTarget{}, err
	}
	target := CreateTarget{Path: absolute}
	info, err := os.Stat(absolute)
	if errors.Is(err, os.ErrNotExist) {
		return target, nil
	}
	if err != nil {
		return target, err
	}
	if !info.IsDir() {
		return target, fmt.Errorf("creation target is not a directory: %s", absolute)
	}
	target.Exists = true
	directory, err := os.Open(absolute)
	if err != nil {
		return target, err
	}
	entries, readErr := directory.ReadDir(5)
	closeErr := directory.Close()
	if readErr != nil && !errors.Is(readErr, io.EOF) && !errors.Is(readErr, os.ErrNotExist) {
		return target, errors.Join(readErr, closeErr)
	}
	if closeErr != nil {
		return target, closeErr
	}
	target.NonEmpty = len(entries) > 0
	for _, entry := range entries {
		target.Entries = append(target.Entries, entry.Name())
	}
	_, err = os.Stat(filepath.Join(absolute, documentpkg.ManifestFilename))
	switch {
	case err == nil:
		target.ManifestExists = true
		if document, readErr := documentpkg.ManifestFile(absolute).Read(); readErr == nil {
			target.DeclaredPackages = declaredPackageCount(document)
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return target, err
	}
	return target, nil
}

// Create writes a new manifest and lock in opts.Directory, then optionally
// installs the server. It does not prompt; the command boundary owns choices.
func Create(ctx context.Context, opts CreateOptions) (result *CreateResult, resultErr error) {
	root, err := createAbsolute(opts.Directory)
	if err != nil {
		return nil, err
	}
	target, err := InspectCreateTarget(root)
	if err != nil {
		return nil, err
	}
	if target.ManifestExists && !opts.Force {
		return nil, fmt.Errorf(
			"lucy.yaml exists in %s; pass --force to replace declared intent",
			target.Path,
		)
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	writer, err := workspacewrite.Lock(root)
	if err != nil {
		return nil, err
	}
	releaseWriter := func() error {
		if writer == nil {
			return nil
		}
		closeErr := writer.Close()
		writer = nil
		return closeErr
	}
	defer func() { resultErr = errors.Join(resultErr, releaseWriter()) }()
	if err := workspacewrite.RecoverInstall(root); err != nil {
		return nil, err
	}
	if err := filetrack.RecoverPending(root); err != nil {
		return nil, err
	}
	manifestExisted := false
	switch _, err := os.Stat(filepath.Join(root, documentpkg.ManifestFilename)); {
	case err == nil:
		manifestExisted = true
		if !opts.Force {
			return nil, fmt.Errorf(
				"lucy.yaml exists in %s; pass --force to replace declared intent",
				root,
			)
		}
	case errors.Is(err, os.ErrNotExist):
	default:
		return nil, err
	}
	var replaced *documentpkg.Manifest
	if opts.Force && manifestExisted {
		replaced, _ = documentpkg.ManifestFile(root).Read()
	}
	old, err := documentpkg.LockFile(root).Read()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if errors.Is(err, os.ErrNotExist) {
		old = nil
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = filepath.Base(root)
	}
	document := newCreateDocument(opts, name)
	if err := document.Normalize(); err != nil {
		return nil, newCreateResolveError(root, document, err)
	}
	resolved, resolution, err := install.Resolve(
		ctx,
		root,
		document,
		old,
		install.Options{AcceptBroken: false, Trace: opts.Trace},
	)
	if err != nil {
		return nil, newCreateResolveError(root, document, err)
	}
	manifestFile := documentpkg.ManifestFile(root)
	if opts.Force {
		manifestFile = manifestFile.WithoutMerge()
	}
	if err := manifestFile.Write(document); err != nil {
		return nil, &CreatePublishError{Workspace: root, Err: err}
	}
	publishedLock := resolved != old
	if publishedLock {
		if err := documentpkg.LockFile(root).Write(resolved); err != nil {
			return nil, &CreatePublishError{
				Workspace:       root,
				ManifestWritten: true,
				Err:             err,
			}
		}
	}
	result = &CreateResult{
		Workspace:        root,
		NonEmpty:         target.NonEmpty,
		ReplacedManifest: manifestExisted,
		Manifest:         document,
		Lock:             resolved,
	}
	if resolution != nil {
		result.Result = *resolution
	}
	result.Schema = install.ResultSchema
	result.Kind = CreateKind
	if result.Mode == "" {
		result.Mode = install.ModeResolve
	}
	if result.Mode == install.ModeResolve {
		result.Reason = "new workspace"
	}
	result.Server = install.ServerChange{
		Change:    install.ChangeAdded,
		Product:   resolved.Server.Product,
		Minecraft: resolved.Server.Minecraft,
		Version:   resolved.Server.CoreVersion,
	}
	result.TotalPackages = len(resolved.Packages)
	result.PublishedManifest = true
	result.PublishedLock = publishedLock
	if replaced != nil {
		if count := declaredPackageCount(replaced); count > 0 {
			result.Warnings = append(result.Warnings, fmt.Errorf(
				"replaced an existing manifest that declared %s",
				packageCountLabel(count),
			))
		}
	}
	if opts.Install {
		// Installation acquires the workspace writer lock itself.
		if err := releaseWriter(); err != nil {
			return result, err
		}
		installOptions := opts.InstallOptions
		installOptions.Trace = opts.Trace
		installOptions.ExpectedManifest = resolved.ManifestFingerprint
		installed, installErr := install.Sync(
			ctx,
			root,
			document,
			resolved,
			installOptions,
		)
		if installed != nil {
			result.Files = installed.Files
			result.Fetch.Cached += installed.Fetch.Cached
			result.Fetch.Downloaded += installed.Fetch.Downloaded
			result.Fetch.Bytes += installed.Fetch.Bytes
			result.Phases = append(result.Phases, installed.Phases...)
			result.Diagnostics = append(result.Diagnostics, installed.Diagnostics...)
			result.Java = installed.Java
			result.Packages = installed.Packages
			result.Warnings = append(result.Warnings, installed.Warnings...)
			result.PublishedManifest = result.PublishedManifest || installed.PublishedManifest
			result.PublishedLock = result.PublishedLock || installed.PublishedLock
		}
		if installErr != nil {
			if _, committed := errors.AsType[*install.CommittedError](installErr); committed {
				result.Installed = true
			}
			result.finalizeWarnings()
			return result, &CreateInstallError{Workspace: root, Err: installErr}
		}
		result.Installed = true
	}
	result.finalizeWarnings()
	return result, nil
}

func (r *CreateResult) finalizeWarnings() {
	r.WarningMessages = warningStrings(r.Warnings)
	if r.NonEmpty {
		r.WarningMessages = append(
			r.WarningMessages,
			"the target directory was not empty; existing files are left in place",
		)
	}
}

func newCreateResolveError(
	root string,
	document *documentpkg.Manifest,
	err error,
) *CreateResolveError {
	return &CreateResolveError{
		Workspace: root,
		Product:   document.Server.Core.Product,
		Minecraft: document.Server.Minecraft,
		Selector:  document.Server.Core.ProductVersion,
		Err:       err,
	}
}

func newCreateDocument(opts CreateOptions, name string) *documentpkg.Manifest {
	product := serverpkg.ProductMinecraft
	if productName := strings.TrimSpace(opts.Product); productName != "" {
		if parsed, known := serverpkg.ParseProduct(productName); known {
			product = parsed
		} else {
			product = serverpkg.Product(strings.ToLower(productName))
		}
	}
	document := &documentpkg.Manifest{
		SchemaVersion: documentpkg.ManifestSchemaVersion,
		Name:          name,
		Server: documentpkg.ManifestServer{
			Minecraft: version.Version(strings.TrimSpace(opts.Minecraft)),
			Directory: ".",
			Core: documentpkg.ManifestCore{
				Product:        product,
				ProductVersion: version.Version(strings.TrimSpace(opts.ProductVersion)),
			},
			Packages: map[catalog.Ecosystem]map[string]documentpkg.Requirement{},
		},
	}
	if opts.MCDRVersion != "" {
		document.MCDR = &documentpkg.ManifestMCDR{
			Directory: ".",
			Version:   version.Version(strings.TrimSpace(opts.MCDRVersion)),
			Packages:  map[string]documentpkg.Requirement{},
		}
	}
	return document
}

func declaredPackageCount(document *documentpkg.Manifest) int {
	if document == nil {
		return 0
	}
	count := 0
	for _, packages := range document.Server.Packages {
		count += len(packages)
	}
	if document.MCDR != nil {
		count += len(document.MCDR.Packages)
	}
	return count
}

func warningStrings(warnings []error) []string {
	messages := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if warning != nil {
			messages = append(messages, warning.Error())
		}
	}
	return messages
}

func packageCountLabel(count int) string {
	if count == 1 {
		return "1 package"
	}
	return fmt.Sprintf("%d packages", count)
}

// createAbsolute expands a leading home marker before resolving a creation
// path. The interactive form cannot rely on a shell to do it.
func createAbsolute(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if path == "~" {
			path = home
		} else {
			path = filepath.Join(home, strings.TrimPrefix(path, "~/"))
		}
	}
	return filepath.Abs(path)
}
