// Package workspace observes a Minecraft server directory (i.e., a workspace).
//
// The package collects the following information:
//
//   - the server runtime that runs in the directory
//   - the directories that hold content packages
//   - the content packages that are installed
//   - the running state of the server process
//
// Observe returns a Workspace pointer. A Workspace is one local observation:
// ServerDir, Wrappers, Discovery, Packages, UnidentifiedPackageFiles, and
// Warnings store the facts, and every other member is a method over them.
//
// Observation is local-only: the package reads the server directory and the
// installed artifacts, and never contacts upstream services. Resolving the
// upstream identity of a package is a separate step.
package workspace

import (
	"errors"
	"path/filepath"

	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace/discovery"
)

const sessionLockName = "session.lock"

// Workspace is one consistent observation of a server directory.
type Workspace struct {
	// ServerDir is the server directory that the observation describes.
	// If MCDR manages the anchor, then ServerDir is the MCDR working
	// directory.
	ServerDir string      `json:"root"`
	Wrappers  WrapperInfo `json:"wrappers"`

	// Warnings records the recoverable problems seen while observing the
	// directory, such as MCDR version-detection failures. It stays out of
	// the JSON wire format; CLI presentation reports the entries once per
	// consumed observation.
	Warnings []error `json:"-"`

	// Discovery holds the observed installations, ownership diagnostics,
	// and the runtime selected from the request and the applicable
	// declaration. Presentation projects this result explicitly.
	Discovery discovery.Result `json:"-"`

	// Packages contains identities associated with installed package files.
	// Observation fills it from descriptors; identification can add hash matches.
	Packages []DiscoveredPackage `json:"packages"`

	// UnidentifiedPackageFiles lists JAR paths without a known package identity.
	// A missing or unreadable descriptor does not establish that a file is a
	// valid package.
	UnidentifiedPackageFiles []string `json:"-"`

	// declared is the applicable declaration derived from the manifest and
	// lock, if any. Later selections keep its authority.
	declared *discovery.Declaration

	// selectionErr records why the current selection is not valid. A nil
	// error means the observation selected a runtime.
	selectionErr error
}

// Observe builds one observation of root. The request carries an explicit
// entry point and the selection policy. Observe derives the applicable
// declaration from the local manifest and lock itself, discovers
// installations, verifies receipt ownership, selects a runtime, and then
// observes the package directories of the selected runtime.
//
// Selection happens before package observation so package directories always
// belong to the runtime that the observation reports.
func Observe(root string, request discovery.Request) *Workspace {
	return observe(root, request, true)
}

// observe is the shared observation pipeline. withPackages is false only for
// activity checks, which need the selected runtime but not package files.
func observe(root string, request discovery.Request, withPackages bool) *Workspace {
	anchor, err := filepath.Abs(root)
	if err != nil {
		return &Workspace{}
	}
	wrappers, warnings := detectWrappers(anchor)
	declaration, locked := applicableDeclaration(anchor)

	ws := &Workspace{
		ServerDir: resolveServerDir(anchor, wrappers),
		Wrappers:  wrappers,
		Warnings:  warnings,
		declared:  declaration,
	}
	if locked != nil {
		ws.ServerDir = filepath.Join(anchor, filepath.FromSlash(locked.Server.Directory))
	}
	ws.Discovery = discovery.Discover(ws.ServerDir)
	ws.Discovery.ObserveOwnership(anchor)

	request.Declared = declaration
	if request.EntryPoint != "" && !filepath.IsAbs(request.EntryPoint) {
		request.EntryPoint = filepath.Join(anchor, request.EntryPoint)
	}
	ws.selectionErr = ws.Discovery.Select(request)

	if withPackages {
		ws.ObservePackages()
	}
	return ws
}

// Select changes the runtime selection of this observation. It preserves the
// declaration derived during observation and never rescans installations.
func (ws *Workspace) Select(request discovery.Request) error {
	if ws == nil {
		return errors.New("workspace: no observation")
	}
	if request.Declared == nil {
		request.Declared = ws.declared
	}
	ws.selectionErr = ws.Discovery.Select(request)
	return ws.selectionErr
}

// SelectionError reports why this observation has no valid runtime selection.
// It is nil when a runtime was selected.
func (ws *Workspace) SelectionError() error {
	if ws == nil {
		return nil
	}
	return ws.selectionErr
}

// ObservePackages observes the package directories of the selected runtime
// and replaces Packages and UnidentifiedPackageFiles. Callers use it after an
// interactive selection to finish package observation without scanning the
// runtime again.
func (ws *Workspace) ObservePackages() {
	if ws == nil {
		return
	}
	ws.Packages, ws.UnidentifiedPackageFiles = discoverPackages(
		ws.PackageDirs(),
		mcdrPluginDirs(ws.Wrappers),
	)
}

// applicableDeclaration builds the runtime declaration from a readable
// manifest and a valid lock whose runtime identity agrees with the manifest.
// Package-only intent changes do not invalidate runtime authority.
func applicableDeclaration(
	root string,
) (*discovery.Declaration, *lockfile.Document) {
	declared, err := manifest.Read(root)
	if err != nil {
		return nil, nil
	}
	locked, err := lockfile.Read(root)
	if err != nil {
		return nil, nil
	}
	if declared.Server.Directory != locked.Server.Directory ||
		declared.Server.Minecraft != locked.Server.Minecraft ||
		declared.Server.Core.Product != locked.Server.Product ||
		(concreteRuntimeVersion(declared.Server.Core.ProductVersion) &&
			declared.Server.Core.ProductVersion != locked.Server.CoreVersion) {
		return nil, nil
	}
	return &discovery.Declaration{
		Product:     locked.Server.Product,
		Minecraft:   locked.Server.Minecraft,
		CoreVersion: locked.Server.CoreVersion,
	}, locked
}

// resolveServerDir returns the directory that holds the server. Only MCDR
// moves the server away from the anchor. MCDR uses its configured working
// directory.
func resolveServerDir(anchor string, wrappers WrapperInfo) string {
	if wrappers.MCDR != nil {
		wd := wrappers.MCDR.Config.WorkingDirectory
		if filepath.IsAbs(wd) {
			return wd
		}
		return filepath.Join(anchor, wd)
	}
	return anchor
}

func concreteRuntimeVersion(version versionpkg.Version) bool {
	switch version {
	case "", versionpkg.None, versionpkg.Unknown, versionpkg.Any,
		versionpkg.Stable, versionpkg.Beta:
		return false
	default:
		return true
	}
}

func mcdrPluginDirs(wrappers WrapperInfo) []string {
	if wrappers.MCDR == nil {
		return nil
	}
	return wrappers.MCDR.Config.PluginDirectories
}

// Runtime returns the selected runtime, or nil when the observation did not
// settle on one. Every non-nil result satisfies IsValid.
func (ws *Workspace) Runtime() *discovery.Selection {
	if ws == nil {
		return nil
	}
	return ws.Discovery.Selected()
}

// Running reports whether the server runs at this moment. The check probes
// an exclusive lock on the session file. Running performs I/O on every call,
// because activity changes over time. Running returns false when no single
// runtime exists, or when the save location is unknown.
func (ws *Workspace) Running() bool {
	if ws == nil || ws.Runtime() == nil {
		return false
	}
	saveDir := ws.SaveDir()
	if saveDir == "" {
		return false
	}
	return checkSessionLock(filepath.Join(saveDir, sessionLockName))
}
