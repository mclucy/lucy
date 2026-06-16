// Package probe provides functionality to gather and manage server information
// for a Minecraft server. It includes methods to retrieve server configuration,
// mod list, executable information, and other relevant details. The package
// utilizes memoization to avoid redundant calculations and resolve any data
// dependencies issues. Therefore, all probe functions are 100% concurrent-safe.
//
// The main exposed function is ServerInfo, which returns a comprehensive
// ServerInfo struct containing all the gathered information. To avoid side
// effects, the ServerInfo struct is returned as a copy, rather than reference.
package probe

import (
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/exttype"
	"gopkg.in/ini.v1"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
)

var (
	serverInfoMu    sync.RWMutex
	serverInfoCache Workspace
	serverInfoReady bool

	resetProbeExecCache     = func() {}
	resetProbeFileLockCache = func() {}
)

// ServerInfo is the exposed function for external packages to get serverInfo.
// The value is cached after the first build, and read access is blocked while
// Rebuild refreshes the cache.
func ServerInfo() Workspace {
	serverInfoMu.RLock()
	if serverInfoReady {
		cached := serverInfoCache
		serverInfoMu.RUnlock()
		return cached
	}
	serverInfoMu.RUnlock()

	serverInfoMu.Lock()
	defer serverInfoMu.Unlock()

	if !serverInfoReady {
		resetProbeMemoizedStateLocked()
		serverInfoCache = buildServerInfo()
		serverInfoReady = true
	}

	return serverInfoCache
}

// Rebuild forces ServerInfo to be regenerated and blocks all readers while
// rebuilding.
func Rebuild() {
	serverInfoMu.Lock()
	defer serverInfoMu.Unlock()

	resetProbeMemoizedStateLocked()
	serverInfoCache = buildServerInfo()
	serverInfoReady = true
}

// InvalidateServerInfo marks the cached ServerInfo as stale so the next call
// to ServerInfo() will re-probe the server state. This is useful after
// installing packages (e.g., identity packages like Fabric) to refresh the
// topology cache without forcing an immediate rebuild.
func InvalidateServerInfo() {
	serverInfoMu.Lock()
	defer serverInfoMu.Unlock()
	serverInfoReady = false
}

// ServerInfoAt probes an explicit working directory without replacing the
// current process-global ServerInfo cache. This is intended for init-style
// takeover discovery where the caller may need rich observed state for a target
// directory that is not the current process working directory.
func ServerInfoAt(workDir string) Workspace {
	serverInfoMu.Lock()
	defer serverInfoMu.Unlock()

	return buildServerInfoAtLocked(workDir, false)
}

// RefreshServerInfo refreshes probed state for workDir. When workDir matches
// the current process working directory, this rebuilds the shared cache so
// future ServerInfo() calls observe the new state. Otherwise it performs an ad
// hoc reprobe and returns the refreshed observation without mutating the shared
// cache.
func RefreshServerInfo(workDir string) Workspace {
	serverInfoMu.Lock()
	defer serverInfoMu.Unlock()

	return buildServerInfoAtLocked(workDir, true)
}

// DetectPackages analyzes a local artifact file and returns packages detected
// from its embedded metadata.
func DetectPackages(filePath string) []types.Package {
	infos, err := artifact.Analyze(filePath)
	if err != nil || len(infos) == 0 {
		return nil
	}
	return artifactInfoToPackage(infos)
}

func resetProbeMemoizedStateLocked() {
	modPaths = tools.Memoize(buildModPaths)
	getEnvironment = tools.Memoize(buildEnvironment)
	workPath = tools.Memoize(buildWorkPath)
	serverProperties = tools.Memoize(buildServerProperties)
	savePath = tools.Memoize(buildSavePath)
	installedPackages = tools.Memoize(buildInstalledPackages)
	resetProbeExecCache()
	resetProbeFileLockCache()
}

func buildServerInfoAtLocked(
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

	savedCache := serverInfoCache
	savedReady := serverInfoReady
	shouldRestoreCache := true
	defer func() {
		resetProbeMemoizedStateLocked()
		if shouldRestoreCache {
			serverInfoCache = savedCache
			serverInfoReady = savedReady
		}
	}()

	if err := os.Chdir(target); err != nil {
		return Workspace{}
	}
	defer func() {
		_ = os.Chdir(originalWD)
	}()

	resetProbeMemoizedStateLocked()
	info := buildServerInfo()

	if persistWhenCurrent && sameProbePath(target, originalTarget) {
		serverInfoCache = info
		serverInfoReady = true
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

// buildServerInfo builds the server information by performing several checks
// and gathering data from various sources. It uses goroutines to perform these
// tasks concurrently and a sync.Mutex to ensure thread-safe updates to the
// serverInfo struct.
func buildServerInfo() Workspace {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var serverInfo Workspace

	// Environment stage
	wg.Add(1)
	go func() {
		defer wg.Done()
		env := getEnvironment()
		mu.Lock()
		serverInfo.Environments = env
		mu.Unlock()
	}()

	// Server Work Path
	wg.Add(1)
	go func() {
		defer wg.Done()
		workPath := workPath()
		mu.Lock()
		serverInfo.Root = workPath
		mu.Unlock()
	}()

	// Executable Stage
	wg.Add(1)
	go func() {
		defer wg.Done()
		executable := getExecutableInfo()
		mu.Lock()
		serverInfo.Runtime = executable
		mu.Unlock()
	}()

	// Mod Path
	wg.Add(1)
	go func() {
		defer wg.Done()
		modPath := modPaths()
		mu.Lock()
		serverInfo.ModPath = modPath
		mu.Unlock()
	}()

	// Mod List
	wg.Add(1)
	go func() {
		defer wg.Done()
		packages := installedPackages()
		mu.Lock()
		serverInfo.Packages = packages
		mu.Unlock()
	}()

	// Save Path
	wg.Add(1)
	go func() {
		defer wg.Done()
		savePath := savePath()
		mu.Lock()
		serverInfo.SavePath = savePath
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
		serverInfo.Activity = activity
		mu.Unlock()
	}()

	wg.Wait()
	serverInfo.Packages = finalizeProbedRuntime(
		serverInfo.Runtime,
		serverInfo.Packages,
	)

	return serverInfo
}

// Some functions that gets a single piece of information. They are not exported,
// as ServerInfo() applies a memoization mechanism. Every time a serverInfo
// is needed, just call ServerInfo() without the concern of redundant calculation.

func buildModPaths() (paths []string) {
	exec := getExecutableInfo()
	if exec == nil {
		return
	}

	return packageSearchPaths(exec, workPath())
}

var modPaths = tools.Memoize(buildModPaths)

var getEnvironment = tools.Memoize(buildEnvironment)

func buildWorkPath() string {
	env := getEnvironment()
	if env.Mcdr != nil {
		return env.Mcdr.Config.WorkingDirectory
	}
	return "."
}

var workPath = tools.Memoize(buildWorkPath)

func buildServerProperties() exttype.FileMinecraftServerProperties {
	exec := getExecutableInfo()
	propertiesPath := path.Join(workPath(), "server.properties")
	file, err := ini.Load(propertiesPath)
	if err != nil {
		if exec != UnknownExecutable {
			logger.Info("this server is missing a server.properties")
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

var serverProperties = tools.Memoize(buildServerProperties)

func buildSavePath() string {
	serverProperties := serverProperties()
	if serverProperties == nil {
		return ""
	}
	levelName := serverProperties["level-name"]
	return path.Join(workPath(), levelName)
}

var savePath = tools.Memoize(buildSavePath)

// artifactInfoToPackage converts artifact detection results into the legacy
// types.Package format used by PackageIndex. This is temporary glue until
// types.Package is fully replaced.
func artifactInfoToPackage(infos []artifact.ArtifactInfo) []types.Package {
	if len(infos) == 0 {
		return nil
	}
	pkgs := make([]types.Package, 0, len(infos))
	for _, info := range infos {
		pkg := types.Package{
			Id: types.VersionedPackageRef{
				PackageRef: types.PackageRef{
					Platform: info.Ref.Platform,
					Name:     info.Ref.Name,
				},
				Version: info.Version,
			},
			Supports:    info.Supports,
			Information: &info.Metadata,
			Local: &types.PackageInstallation{
				Path: info.FilePath,
			},
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
						Embedded:   dep.Embedded,
					},
				)
			}
			pkg.Dependencies = &types.PackageDependencies{
				Value: deps,
			}
		}
		pkgs = append(pkgs, pkg)
	}
	return pkgs
}

func buildInstalledPackages() (mods []types.Package) {
	idx := NewPackageIndex()
	var mu sync.Mutex

	paths := modPaths()
	for _, modPath := range paths {
		jarFiles, err := findJar(modPath)
		if err != nil {
			logger.Warn(err)
			logger.Info("cannot read the mod directory")
			continue
		}

		var wg sync.WaitGroup
		for _, jarPath := range jarFiles {
			wg.Add(1)
			go func(path string) {
				defer wg.Done()

				analyzed, err := artifact.Analyze(path)
				if err != nil || len(analyzed) == 0 {
					return
				}
				pkgs := artifactInfoToPackage(analyzed)

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
				logger.Warn(err)
				logger.Info("cannot read the MCDR plugin directory")
				continue
			}
			for _, pluginFile := range pluginFiles {
				analyzed, err := artifact.Analyze(pluginFile)
				if err == nil && len(analyzed) > 0 {
					pkgs := artifactInfoToPackage(analyzed)
					idx.Merge(pkgs)
				}
			}
		}
	}

	return idx.Packages()
}

var installedPackages = tools.Memoize(buildInstalledPackages)
