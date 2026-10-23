package cli

import (
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	serverpkg "github.com/mclucy/lucy/server"
	versionpkg "github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
	"github.com/mclucy/lucy/workspace/discovery"
)

const (
	statusDocumentSchema = 1
	statusDocumentKind   = "status"

	statusSourceLive = "live"
	statusSourceLock = "lock"

	statusActivityRunning = "running"
	statusActivityStopped = "stopped"
	statusActivityUnknown = "unknown"

	statusErrorLockMissing     = "status.lock_missing"
	statusErrorReadFailed      = "status.read_failed"
	statusErrorSelectionFailed = "status.selection_failed"
)

// statusDocument is the one wire format both live observation and lock
// rendering produce. Entries are never folded here; the human renderer bounds
// its own view.
type statusDocument struct {
	Schema   int             `json:"schema"`
	Kind     string          `json:"kind"`
	Source   string          `json:"source"`
	Root     string          `json:"root"`
	Server   *statusServer   `json:"server"`
	Packages []statusPackage `json:"packages"`
	MCDR     *statusMCDR     `json:"mcdr,omitempty"`
	Warnings []string        `json:"warnings"`
}

type statusServer struct {
	Product              statusProduct     `json:"product"`
	Minecraft            string            `json:"minecraft"`
	EntryPoint           string            `json:"entry_point"`
	Activity             string            `json:"activity"`
	Components           []statusComponent `json:"components"`
	CompatibleEcosystems []statusEcosystem `json:"compatible_ecosystems"`
	PackageDirectories   []string          `json:"package_directories"`
}

type statusProduct struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Ecosystem string `json:"ecosystem"`
}

type statusComponent struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Provider  string `json:"provider"`
	Ecosystem string `json:"ecosystem"`
}

type statusEcosystem struct {
	Ecosystem     string `json:"ecosystem"`
	Compatibility string `json:"compatibility"`
}

type statusPackage struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Provider    string `json:"provider"`
	Ecosystem   string `json:"ecosystem"`
	Environment string `json:"environment"`
	Path        string `json:"path"`
}

type statusMCDR struct {
	Directory string `json:"directory"`
	Version   string `json:"version"`
	Python    string `json:"python"`
	Target    string `json:"target"`
}

func newStatusDocument(source, root string) statusDocument {
	return statusDocument{
		Schema:   statusDocumentSchema,
		Kind:     statusDocumentKind,
		Source:   source,
		Root:     root,
		Packages: []statusPackage{},
		Warnings: []string{},
	}
}

func newLiveStatusDocument(
	observed *workspace.Workspace,
	root string,
) statusDocument {
	document := newStatusDocument(statusSourceLive, root)
	if observed == nil {
		return document
	}
	selection := observed.Runtime()
	if selection != nil {
		document.Server = statusServerFromObservation(observed, selection, root)
	}
	document.Packages = statusPackagesFromObservation(
		observed.Packages,
		selection,
		root,
	)
	document.MCDR = statusMCDRFromObservation(observed, root)
	return document
}

func statusServerFromObservation(
	observed *workspace.Workspace,
	selection *discovery.Selection,
	root string,
) *statusServer {
	entryPoint := ""
	if selection.EntryPoint != nil {
		entryPoint = statusRelativePath(root, selection.EntryPoint.Path)
	}
	activity := statusActivityStopped
	if observed.Running() {
		activity = statusActivityRunning
	}
	return &statusServer{
		Product:              statusProductFromSelection(selection),
		Minecraft:            selection.MinecraftVersion().String(),
		EntryPoint:           entryPoint,
		Activity:             activity,
		Components:           statusComponents(selectedRuntimeComponents(selection)),
		CompatibleEcosystems: statusCompatibleEcosystems(observed.ServedEcosystems()),
		PackageDirectories:   statusRelativePaths(root, observed.PackageDirs()),
	}
}

// statusProductFromSelection reports the catalogued product identity rather
// than the observed artifact identity. A launcher that records its full
// filename as its version still reports the short product name.
func statusProductFromSelection(
	selection *discovery.Selection,
) statusProduct {
	if selection == nil || selection.Installation == nil {
		return statusProduct{}
	}
	identity := selection.Installation.Identity
	primary := identity.Primary
	name := primary.Name.String()
	ecosystem := primary.Ecosystem
	if match, ok := serverpkg.NormalizeProduct(primary); ok {
		name = match.Product.String()
		if ecosystem == catalog.EcosystemUnspecified {
			ecosystem = match.Ecosystem
		}
	}
	if ecosystem == catalog.EcosystemUnspecified {
		ecosystem = identity.Ecosystem()
	}
	return statusProduct{
		Name:      name,
		Version:   primary.Version.String(),
		Ecosystem: ecosystem.String(),
	}
}

func statusComponents(
	components []catalog.VersionedRef,
) []statusComponent {
	result := make([]statusComponent, 0, len(components))
	for _, component := range components {
		result = append(result, statusComponent{
			Name:      component.Name.String(),
			Version:   component.Version.String(),
			Provider:  component.Provider.String(),
			Ecosystem: component.Ecosystem.String(),
		})
	}
	return result
}

func statusCompatibleEcosystems(
	offers []workspace.ServedEcosystem,
) []statusEcosystem {
	result := make([]statusEcosystem, 0, len(offers))
	for _, offer := range offers {
		result = append(result, statusEcosystem{
			Ecosystem:     offer.Ecosystem.String(),
			Compatibility: string(offer.Compatibility),
		})
	}
	return result
}

// statusPackagesFromObservation projects installed content packages. Runtime
// components belong in the server document, and server products are not
// content packages, so both are excluded from the package list.
func statusPackagesFromObservation(
	packages []workspace.DiscoveredPackage,
	selection *discovery.Selection,
	root string,
) []statusPackage {
	result := make([]statusPackage, 0, len(packages))
	runtimeComponents := map[string]struct{}{}
	if selection != nil {
		for _, component := range selectedRuntimeComponents(selection) {
			runtimeComponents[component.StringBase()] = struct{}{}
		}
	}
	for _, pkg := range packages {
		if _, isComponent := runtimeComponents[pkg.ID.StringBase()]; isComponent {
			continue
		}
		if serverpkg.IsProduct(pkg.ID) {
			continue
		}
		environment := serverpkg.EnvironmentServer
		if pkg.ID.Ecosystem == catalog.EcosystemMCDR {
			environment = serverpkg.EnvironmentMCDR
		}
		result = append(result, statusPackage{
			Name:        pkg.ID.Name.String(),
			Version:     pkg.ID.Version.String(),
			Provider:    pkg.ID.Provider.String(),
			Ecosystem:   pkg.ID.Ecosystem.String(),
			Environment: string(environment),
			Path:        statusRelativePath(root, pkg.Path),
		})
	}
	return result
}

func statusMCDRFromObservation(
	observed *workspace.Workspace,
	root string,
) *statusMCDR {
	if observed == nil || observed.Wrappers.MCDR == nil {
		return nil
	}
	return &statusMCDR{
		Directory: statusRelativePath(root, root),
		Version:   observed.Wrappers.MCDR.Version.String(),
	}
}

func newLockStatusDocument(
	root string,
	lock *lockfile.Document,
) statusDocument {
	document := newStatusDocument(statusSourceLock, root)
	if lock == nil {
		return document
	}
	document.Server = &statusServer{
		Product: statusProduct{
			Name:      lock.Server.Product.String(),
			Version:   lock.Server.CoreVersion.String(),
			Ecosystem: statusLockProductEcosystem(lock).String(),
		},
		Minecraft:            lock.Server.Minecraft.String(),
		EntryPoint:           "",
		Activity:             statusActivityUnknown,
		Components:           statusLockComponents(lock),
		CompatibleEcosystems: statusLockCompatibleEcosystems(lock),
		PackageDirectories:   statusLockPackageDirectories(root, lock),
	}
	if lock.MCDR != nil {
		document.MCDR = &statusMCDR{
			Directory: statusRelativePath(root, lock.MCDR.Directory),
			Version:   lock.MCDR.Version.String(),
			Python:    lock.MCDR.Python,
			Target:    lock.MCDR.Target,
		}
	}
	document.Packages = make([]statusPackage, 0, len(lock.Packages))
	for i := range lock.Packages {
		pkg := &lock.Packages[i]
		document.Packages = append(document.Packages, statusPackage{
			Name:        statusLockPackageName(pkg),
			Version:     pkg.Version.String(),
			Provider:    pkg.Provider.String(),
			Ecosystem:   pkg.Ecosystem.String(),
			Environment: string(pkg.Environment),
			Path:        statusLockPackagePath(root, lock, pkg),
		})
	}
	return document
}

// statusLockComponents reports the runtime components the lock proves. The
// lock records the core version and the Minecraft version, so loader cores
// contribute their loader component exactly as live selection does.
func statusLockComponents(lock *lockfile.Document) []statusComponent {
	components := make([]statusComponent, 0, 2)
	components = append(components, statusComponent{
		Name:      "minecraft",
		Version:   lock.Server.Minecraft.String(),
		Ecosystem: catalog.EcosystemMinecraft.String(),
	})
	var (
		name      string
		ecosystem catalog.Ecosystem
	)
	switch lock.Server.Product {
	case serverpkg.ProductFabric:
		name, ecosystem = "fabric-loader", catalog.EcosystemFabric
	case serverpkg.ProductForge:
		name, ecosystem = "forge", catalog.EcosystemForge
	case serverpkg.ProductNeoForge:
		name, ecosystem = "neoforge", catalog.EcosystemNeoForge
	default:
		return components
	}
	if !statusConcreteVersion(lock.Server.CoreVersion) {
		return components
	}
	return append(components, statusComponent{
		Name:      name,
		Version:   lock.Server.CoreVersion.String(),
		Ecosystem: ecosystem.String(),
	})
}

// Only enabled packages can contribute compatibility evidence.
func statusLockCompatibleEcosystems(
	lock *lockfile.Document,
) []statusEcosystem {
	result := make([]statusEcosystem, 0, 3)
	seen := make(map[catalog.Ecosystem]bool, 3)
	add := func(ecosystem catalog.Ecosystem, compatibility workspace.Compatibility) {
		if !ecosystem.Valid() ||
			ecosystem == catalog.EcosystemUnspecified ||
			ecosystem == catalog.EcosystemMinecraft ||
			seen[ecosystem] {
			return
		}
		seen[ecosystem] = true
		result = append(result, statusEcosystem{
			Ecosystem:     ecosystem.String(),
			Compatibility: string(compatibility),
		})
	}
	productEcosystem := statusLockProductEcosystem(lock)
	connectorBridge := statusLockHasConnectorBridge(lock)
	add(productEcosystem, workspace.CompatFull)
	for i := range lock.Packages {
		pkg := &lock.Packages[i]
		if !pkg.Enabled() || pkg.Environment != serverpkg.EnvironmentServer {
			continue
		}
		compatibility := workspace.CompatFull
		if connectorBridge &&
			pkg.Ecosystem == catalog.EcosystemFabric &&
			(productEcosystem == catalog.EcosystemForge ||
				productEcosystem == catalog.EcosystemNeoForge) {
			compatibility = workspace.CompatDegraded
		}
		add(pkg.Ecosystem, compatibility)
	}
	if connectorBridge &&
		(productEcosystem == catalog.EcosystemForge ||
			productEcosystem == catalog.EcosystemNeoForge) {
		add(catalog.EcosystemFabric, workspace.CompatDegraded)
	}
	if seen[catalog.EcosystemPaper] {
		add(catalog.EcosystemBukkit, workspace.CompatFull)
	}
	return result
}

// statusLockHasConnectorBridge reports the same package roster the live
// workspace uses to offer degraded Fabric compatibility. Locked packages may
// name the project through a binding or only expose the module identity.
func statusLockHasConnectorBridge(lock *lockfile.Document) bool {
	for i := range lock.Packages {
		pkg := &lock.Packages[i]
		if !pkg.Enabled() || pkg.Environment != serverpkg.EnvironmentServer {
			continue
		}
		if pkg.Ecosystem != catalog.EcosystemForge &&
			pkg.Ecosystem != catalog.EcosystemNeoForge {
			continue
		}
		if statusIsConnectorName(statusLockPackageName(pkg)) {
			return true
		}
		for _, module := range pkg.Modules {
			if statusIsConnectorName(module.ID) {
				return true
			}
		}
	}
	return false
}

func statusIsConnectorName(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "sinytra-connector", "connector", "connectormod":
		return true
	default:
		return false
	}
}

// statusLockPackageDirectories lists the directories locked packages land in.
// A runtime whose ecosystem has a conventional content directory contributes
// that directory even when the lock selects no packages.
func statusLockPackageDirectories(
	root string,
	lock *lockfile.Document,
) []string {
	result := make([]string, 0, 4)
	seen := make(map[string]bool, 4)
	add := func(directory string) {
		if directory == "" {
			return
		}
		relative := statusRelativePath(root, directory)
		if relative == "" || seen[relative] {
			return
		}
		seen[relative] = true
		result = append(result, relative)
	}
	switch statusLockProductEcosystem(lock) {
	case catalog.EcosystemFabric, catalog.EcosystemForge, catalog.EcosystemNeoForge:
		add(filepath.Join(lock.Server.Directory, "mods"))
	case catalog.EcosystemBukkit, catalog.EcosystemPaper:
		add(filepath.Join(lock.Server.Directory, "plugins"))
	}
	for i := range lock.Packages {
		add(statusLockPackageDirectory(lock, &lock.Packages[i]))
	}
	return result
}

// statusLockPackagePath reports the destination the lock's layout assigns to a
// package. It mirrors the install and filetrack placement rules: server
// packages live under mods/, Bukkit-family packages under plugins/, and MCDR
// packages under the MCDR wrapper's plugins/ directory.
func statusLockPackagePath(
	root string,
	lock *lockfile.Document,
	pkg *lockfile.Package,
) string {
	return statusRelativePath(root, statusLockPackageDestination(lock, pkg))
}

func statusLockPackageDirectory(
	lock *lockfile.Document,
	pkg *lockfile.Package,
) string {
	destination := statusLockPackageDestination(lock, pkg)
	if destination == "" {
		return ""
	}
	return filepath.Dir(destination)
}

func statusLockPackageDestination(
	lock *lockfile.Document,
	pkg *lockfile.Package,
) string {
	directory := lock.Server.Directory
	folder := "mods"
	switch pkg.Environment {
	case serverpkg.EnvironmentMCDR:
		if lock.MCDR == nil {
			return ""
		}
		directory, folder = lock.MCDR.Directory, "plugins"
	case serverpkg.EnvironmentServer:
		if pkg.Ecosystem == catalog.EcosystemBukkit ||
			pkg.Ecosystem == catalog.EcosystemPaper {
			folder = "plugins"
		}
	default:
		return ""
	}
	return filepath.Join(directory, folder, pkg.Filename)
}

// statusLockPackageName prefers the project a lock binding names. The lock's
// ProjectID is the opaque provider identity and stays the fallback when no
// binding reference is readable.
func statusLockPackageName(pkg *lockfile.Package) string {
	var fallback string
	for _, binding := range pkg.Requirements {
		_, project, err := manifest.ParseReference(binding.Reference)
		if err != nil || project == "" {
			continue
		}
		if binding.Enabled {
			return project
		}
		if fallback == "" {
			fallback = project
		}
	}
	if fallback != "" {
		return fallback
	}
	if pkg.ProjectID != "" {
		return pkg.ProjectID
	}
	return strings.TrimSuffix(pkg.Filename, filepath.Ext(pkg.Filename))
}

func statusConcreteVersion(version versionpkg.Version) bool {
	return version != "" && !version.CanInfer() && !version.IsInvalid()
}

// Native product support must not narrow to the packages currently declared.
func statusLockProductEcosystem(lock *lockfile.Document) catalog.Ecosystem {
	match, ok := serverpkg.NormalizeProduct(catalog.VersionedRef{
		Name: catalog.Name(lock.Server.Product.String()),
	})
	if ok && match.Ecosystem != catalog.EcosystemUnspecified {
		return match.Ecosystem
	}
	return ecosystemFromLock(lock)
}

// statusRelativePath reports a path relative to the document root. Absolute
// paths outside the root stay relative so the document never mixes roots.
func statusRelativePath(root, path string) string {
	if path == "" {
		return ""
	}
	if !filepath.IsAbs(path) {
		return filepath.ToSlash(filepath.Clean(path))
	}
	relative, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(relative)
}

func statusRelativePaths(root string, paths []string) []string {
	result := make([]string, 0, len(paths))
	for _, path := range paths {
		relative := statusRelativePath(root, path)
		if relative == "" {
			continue
		}
		result = append(result, relative)
	}
	return result
}
