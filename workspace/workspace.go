// Package workspace provides functionality to gather and manage server information
// for a Minecraft server. It includes methods to retrieve server configuration,
// mod list, executable information, and other relevant details. The package
// utilizes memoization to avoid redundant calculations and resolve any data
// dependencies issues. Therefore, all probe functions are 100% concurrent-safe.
//
// The main exposed function is New, which returns a comprehensive
// Workspace struct containing all the gathered information. To avoid side
// effects, the Workspace struct is returned as a copy, rather than reference.
package workspace

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/internal/artifacthash"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/internal/knownpkgs"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/providers/curseforge"
	"github.com/mclucy/lucy/upstream/providers/modrinth"
	"gopkg.in/ini.v1"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
)

var (
	mu    sync.RWMutex
	cache Workspace
	ready bool

	resetProbeExecCache     = func() {}
	resetProbeFileLockCache = func() {}
)

// probeAnchor is the absolute server directory that buildWorkPath returns as
// the workspace root. Set under mu before build() runs.
var probeAnchor string

// New is the exposed function for external packages to get Workspace.
// The value is cached after the first build, and read access is blocked while
// Rebuild refreshes the cache.
func New() Workspace {
	mu.RLock()
	if ready {
		cached := cache
		mu.RUnlock()
		return cached
	}
	mu.RUnlock()

	mu.Lock()
	defer mu.Unlock()

	if !ready {
		resetProbeMemoizedStateLocked()
		probeAnchor, _ = os.Getwd()
		cache = build()
		ready = true
	}

	return cache
}

// Rebuild forces Workspace to be regenerated and blocks all readers while
// rebuilding.
func Rebuild() {
	mu.Lock()
	defer mu.Unlock()

	resetProbeMemoizedStateLocked()
	probeAnchor, _ = os.Getwd()
	cache = build()
	ready = true
}

// Invalidate marks the cached Workspace as stale so the next call
// to New() will re-probe the server state. This is useful after
// installing packages (e.g., identity packages like Fabric) to refresh the
// topology cache without forcing an immediate rebuild.
func Invalidate() {
	mu.Lock()
	defer mu.Unlock()
	ready = false
}

// NewAt probes an explicit working directory without replacing the
// current process-global Workspace cache. This is intended for init-style
// takeover discovery where the caller may need rich observed state for a target
// directory that is not the current process working directory.
func NewAt(workDir string) Workspace {
	mu.Lock()
	defer mu.Unlock()

	return buildAtLocked(workDir, false)
}

// Refresh refreshes probed state for workDir. When workDir matches
// the current process working directory, this rebuilds the shared cache so
// future New() calls observe the new state. Otherwise it performs an ad
// hoc reprobe and returns the refreshed observation without mutating the shared
// cache.
func Refresh(workDir string) Workspace {
	mu.Lock()
	defer mu.Unlock()

	return buildAtLocked(workDir, true)
}

func resetProbeMemoizedStateLocked() {
	modPaths = fn.Memoize(buildModPaths)
	getEnvironment = fn.Memoize(buildEnvironment)
	workPath = fn.Memoize(buildWorkPath)
	serverProperties = fn.Memoize(buildServerProperties)
	savePath = fn.Memoize(buildSavePath)
	installedPackages = fn.Memoize(buildInstalledPackages)
	resetProbeExecCache()
	resetProbeFileLockCache()
}

func buildAtLocked(
	workDir string,
	persistWhenCurrent bool,
) Workspace {
	target, err := filepath.Abs(workDir)
	if err != nil {
		return Workspace{}
	}

	originalWD, err := os.Getwd()
	if err != nil {
		return Workspace{}
	}
	originalTarget, err := filepath.Abs(originalWD)
	if err != nil {
		return Workspace{}
	}

	savedCache := cache
	savedReady := ready
	shouldRestoreCache := true
	defer func() {
		resetProbeMemoizedStateLocked()
		if shouldRestoreCache {
			cache = savedCache
			ready = savedReady
		}
	}()

	probeAnchor = target
	if err := os.Chdir(target); err != nil {
		return Workspace{}
	}
	defer func() {
		_ = os.Chdir(originalWD)
	}()

	resetProbeMemoizedStateLocked()
	info := build()

	if persistWhenCurrent && sameProbePath(target, originalTarget) {
		cache = info
		ready = true
		shouldRestoreCache = false
	}

	return info
}

func sameProbePath(left, right string) bool {
	leftEval, leftErr := filepath.EvalSymlinks(left)
	if leftErr != nil {
		leftEval = left
	}
	rightEval, rightErr := filepath.EvalSymlinks(right)
	if rightErr != nil {
		rightEval = right
	}
	return leftEval == rightEval
}

// build builds the server information by performing several checks
// and gathering data from various sources. It uses goroutines to perform these
// tasks concurrently and a sync.Mutex to ensure thread-safe updates to the
// Workspace struct.
func build() Workspace {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var ws Workspace

	// Environment stage
	wg.Add(1)
	go func() {
		defer wg.Done()
		env := getEnvironment()
		mu.Lock()
		ws.Environments = env
		mu.Unlock()
	}()

	// Server Work Path
	wg.Add(1)
	go func() {
		defer wg.Done()
		workPath := workPath()
		mu.Lock()
		ws.Root = workPath
		mu.Unlock()
	}()

	// Executable Stage
	wg.Add(1)
	go func() {
		defer wg.Done()
		executable := getExecutableInfo()
		mu.Lock()
		ws.Runtime = executable
		mu.Unlock()
	}()

	// Mod Path
	wg.Add(1)
	go func() {
		defer wg.Done()
		modPath := modPaths()
		mu.Lock()
		ws.ModPath = modPath
		mu.Unlock()
	}()

	// Mod List
	wg.Add(1)
	go func() {
		defer wg.Done()
		packages := installedPackages()
		mu.Lock()
		ws.Packages = packages
		mu.Unlock()
	}()

	// Save Path
	wg.Add(1)
	go func() {
		defer wg.Done()
		savePath := savePath()
		mu.Lock()
		ws.SavePath = savePath
		mu.Unlock()
	}()

	// TODO: Check for state.LockFile path
	// However, the local installation method is not determined yet, so this is
	// just a placeholder for now.

	// Check if the server is running
	wg.Add(1)
	go func() {
		defer wg.Done()
		activity := checkServerFileLock()
		mu.Lock()
		ws.Activity = activity
		mu.Unlock()
	}()

	wg.Wait()
	ws.Packages = finalizeProbedRuntime(
		ws.Runtime,
		ws.Packages,
	)
	if ws.Runtime != nil {
		ws.Topology = ws.Runtime.topology
	}

	return ws
}

// Some functions that gets a single piece of information. They are not exported,
// as New() applies a memoization mechanism. Every time a workspace
// is needed, just call Workspace() without the concern of redundant calculation.

func buildModPaths() (paths []string) {
	exec := getExecutableInfo()
	if exec == nil {
		return
	}

	return packageSearchPaths(exec, workPath())
}

var modPaths = fn.Memoize(buildModPaths)

var getEnvironment = fn.Memoize(buildEnvironment)

func buildWorkPath() string {
	env := getEnvironment()
	if env.Mcdr != nil {
		wd := env.Mcdr.Config.WorkingDirectory
		if filepath.IsAbs(wd) {
			return wd
		}
		return filepath.Join(probeAnchor, wd)
	}
	return probeAnchor
}

var workPath = fn.Memoize(buildWorkPath)

func buildServerProperties() fileschema.FileMinecraftServerProperties {
	exec := getExecutableInfo()
	propertiesPath := filepath.Join(workPath(), "server.properties")
	file, err := ini.Load(propertiesPath)
	if err != nil {
		if exec != UnknownExecutable {
			log.Info("this server is missing a server.properties")
		}
		return nil
	}

	properties := make(map[string]string)
	for _, section := range file.Sections() {
		for _, key := range section.Keys() {
			properties[key.Name()] = key.String()
		}
	}

	return properties
}

var serverProperties = fn.Memoize(buildServerProperties)

func buildSavePath() string {
	serverProperties := serverProperties()
	if serverProperties == nil {
		return ""
	}
	levelName := serverProperties["level-name"]
	return filepath.Join(workPath(), levelName)
}

var savePath = fn.Memoize(buildSavePath)

func artifactInfoToDiscoveredPackage(infos []artifact.Info) []types.DiscoveredPackage {
	if len(infos) == 0 {
		return nil
	}
	pkgs := make([]types.DiscoveredPackage, 0, len(infos))
	for _, info := range infos {
		pkg := types.DiscoveredPackage{
			Id: types.VersionedPackageRef{
				PackageRef: types.PackageRef{
					Platform: info.Ref.Platform,
					Name:     info.Ref.Name,
				},
				Version: info.Version,
			},
			Path: info.FilePath,
		}
		if len(info.Dependencies) > 0 {
			deps := make([]types.Dependency, 0, len(info.Dependencies))
			for _, dep := range info.Dependencies {
				deps = append(
					deps, types.Dependency{
						Id: types.VersionedPackageRef{
							PackageRef: types.PackageRef{
								Platform: dep.Ref.Platform,
								Name:     dep.Ref.Name,
							},
						},
						Constraint: dep.Constraint,
						Mandatory:  dep.Mandatory,
						Type:       types.NormalizeDependencyType(dep.Type),
					},
				)
			}
			pkg.Dependencies = types.PackageDependencies{Value: deps}
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs
}

func buildInstalledPackages() (mods []types.DiscoveredPackage) {
	idx := NewPackageIndex()
	var mu sync.Mutex

	sess := knownpkgs.Default().Session()
	resolver := knownPackagesSlugResolver(sess)

	paths := modPaths()
	for _, modPath := range paths {
		jarFiles, err := findJar(modPath)
		if err != nil {
			log.Warn(err)
			log.Info("cannot read the mod directory")
			continue
		}

		var wg sync.WaitGroup
		for _, jarPath := range jarFiles {
			wg.Add(1)
			go func(path string) {
				defer wg.Done()

				analyzed, err := artifact.Analyze(
					path,
					artifact.WithSlugResolver(resolver),
				)
				if err != nil || len(analyzed) == 0 {
					pkg, ok := packageByArtifactHash(path)
					if !ok {
						return
					}
					mu.Lock()
					idx.Add(pkg)
					mu.Unlock()
					return
				}
				pkgs := artifactInfoToDiscoveredPackage(analyzed)

				mu.Lock()
				idx.Merge(pkgs)
				mu.Unlock()
			}(jarPath)
		}
		wg.Wait()
	}

	env := getEnvironment()
	if env.Mcdr != nil {
		for _, dir := range env.Mcdr.Config.PluginDirectories {
			pluginFiles, err := findFileWithExt(dir, ".pyz", ".mcdr")
			if err != nil {
				log.Warn(err)
				log.Info("cannot read the MCDR plugin directory")
				continue
			}
			for _, pluginFile := range pluginFiles {
				analyzed, err := artifact.Analyze(
					pluginFile,
					artifact.WithSlugResolver(resolver),
				)
				if err == nil && len(analyzed) > 0 {
					pkgs := artifactInfoToDiscoveredPackage(analyzed)
					idx.Merge(pkgs)
				}
			}
		}
	}

	return idx.Packages()
}

func packageByArtifactHash(filePath string) (types.DiscoveredPackage, bool) {
	providers := []upstream.ArtifactMapSource{modrinth.Provider}
	if curseforge.Enabled() {
		providers = append(providers, curseforge.Provider)
	}
	for _, mapper := range providers {
		ref, _, ok, err := mapper.PackageByHash(artifacthash.File{Path: filePath})
		if err != nil || !ok || ref.Name == "" {
			continue
		}
		platform := ref.Platform
		if platform == types.PlatformNone || platform == types.PlatformAny {
			platform = types.PlatformForge
		}
		version := ref.Version
		if version == "" {
			version = types.VersionUnknown
		}
		pkgName := ref.Name
		if ref.Scope == types.SourceMCDR {
			pkgName = types.BarePackageName(
				strings.ReplaceAll(string(ref.Name), "_", "-"),
			)
		}
		return types.DiscoveredPackage{
			Id: types.VersionedPackageRef{
				PackageRef: types.PackageRef{
					Platform: platform,
					Name:     pkgName,
				},
				Version: version,
			},
			Path: filePath,
		}, true
	}
	return types.DiscoveredPackage{}, false
}

// knownPackagesSlugResolver returns a slug resolver that consults the knownpkgs
// session for a canonical name matching the detected platform/local name.
//
// On hit, the mapping is promoted into the session cache via Record so that
// subsequent resolutions in the same invocation see the freshly discovered
// mapping without re-querying.
func knownPackagesSlugResolver(session *knownpkgs.Session) artifact.SlugResolver {
	return func(
		ctx context.Context,
		platform types.PlatformId,
		name types.BarePackageName,
	) (types.BarePackageName, error) {
		canonical, src, ok := session.LookupAny(string(name))
		if !ok || canonical == string(name) {
			return name, nil
		}
		// Resolver runs on the local name only, not on file contents — the
		// persisted store already holds this mapping (LookupAny hit it).
		session.Record(src, string(name), "", canonical, "hash")
		return types.BarePackageName(canonical), nil
	}
}

var installedPackages = fn.Memoize(buildInstalledPackages)
