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
	"path"
	"sync"

	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/probe/internal/detector"

	"gopkg.in/ini.v1"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
)

var (
	serverInfoMu    sync.RWMutex
	serverInfoCache types.ServerInfo
	serverInfoReady bool

	resetProbeExecCache     = func() {}
	resetProbeFileLockCache = func() {}
)

// ServerInfo is the exposed function for external packages to get serverInfo.
// The value is cached after the first build, and read access is blocked while
// Rebuild refreshes the cache.
func ServerInfo() types.ServerInfo {
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

// buildServerInfo builds the server information by performing several checks
// and gathering data from various sources. It uses goroutines to perform these
// tasks concurrently and a sync.Mutex to ensure thread-safe updates to the
// serverInfo struct.
func buildServerInfo() types.ServerInfo {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var serverInfo types.ServerInfo

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
		serverInfo.WorkPath = workPath
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

	// TODO: Check for .lucy path
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
	EnrichTopologyFromPackages(serverInfo.Runtime, serverInfo.Packages)

	// Inject runtime identity packages from executable into the package set.
	// This runs AFTER EnrichTopologyFromPackages so topology enrichment can
	// use package names for evidence detection first.
	if serverInfo.Runtime != nil && serverInfo.Runtime.IsValid() {
		idx := NewPackageIndex()
		// First, add all existing packages (they have local paths, so
		// they take precedence under the first-write-wins policy).
		idx.Merge(serverInfo.Packages)
		// Then inject runtime identity packages (no local path — these
		// are runtime identities, not locally installed files).
		for _, rid := range serverInfo.Runtime.RuntimeIdentities {
			if rid.Platform == types.PlatformAny {
				continue
			}
			idx.Add(types.Package{Id: rid})
		}
		serverInfo.Packages = idx.Packages()
	}

	if serverInfo.Runtime != nil && serverInfo.Runtime.Topology == nil {
		serverInfo.Runtime.Topology = &types.RuntimeTopology{}
	}

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

	if exec.Topology != nil && exec.Topology.Resolved() {
		// Topology-driven path discovery
		if exec.Topology.HasCapability(types.CapabilityFabricMods) ||
			exec.Topology.HasCapability(types.CapabilityForgeMods) ||
			exec.Topology.HasCapability(types.CapabilityNeoforgeMods) {
			paths = append(paths, path.Join(workPath(), "mods"))
		}
		if exec.Topology.HasCapability(types.CapabilityBukkitPlugins) {
			paths = append(paths, path.Join(workPath(), "plugins"))
		}
	}
	return
}

var modPaths = tools.Memoize(buildModPaths)

func buildEnvironment() types.EnvironmentInfo {
	return detector.Environment(".")
}

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
		if exec != types.UnknownExecutable {
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

				analyzed := detector.Packages(path)
				if analyzed == nil {
					return
				}

				mu.Lock()
				idx.Merge(analyzed)
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
				analyzed := detector.McdrPlugin(pluginFile)
				if analyzed != nil {
					idx.Merge(analyzed)
				}
			}
		}
	}

	return idx.Packages()
}

var installedPackages = tools.Memoize(buildInstalledPackages)
