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
	"errors"
	"path"
	"sort"
	"sync"

	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/probe/internal/detector"

	"gopkg.in/ini.v1"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
)

// ServerInfo is the exposed function for external packages to get serverInfo.
// As we can assume that the environment does not change while the program is
// running, a sync.Once is used to prevent further calls to this function. Rather,
// the cached serverInfo is used as the return value.
var ServerInfo = tools.Memoize(buildServerInfo)

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
		serverInfo.Executable = executable
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

	// Server Mod Path
	wg.Add(1)
	go func() {
		defer wg.Done()
		modPath := modPaths()
		mu.Lock()
		serverInfo.ModPath = modPath
		mu.Unlock()
	}()

	wg.Wait()
	return serverInfo
}

// Some functions that gets a single piece of information. They are not exported,
// as ServerInfo() applies a memoization mechanism. Every time a serverInfo
// is needed, just call ServerInfo() without the concern of redundant calculation.

var modPaths = tools.Memoize(
	func() (paths []string) {
		if exec := getExecutableInfo(); exec != nil && (exec.ModLoader == types.Fabric || exec.ModLoader == types.Forge) {
			paths = append(paths, path.Join(workPath(), "mods"))
		}
		return
	},
)

var getEnvironment = tools.Memoize(
	func() types.EnvironmentInfo {
		return detector.Environment(".")
	},
)

var workPath = tools.Memoize(
	func() string {
		env := getEnvironment()
		if env.Mcdr != nil {
			return env.Mcdr.WorkingDirectory
		}
		return "."
	},
)

var serverProperties = tools.Memoize(
	func() exttype.FileMinercaftServerProperties {
		exec := getExecutableInfo()
		propertiesPath := path.Join(workPath(), "server.properties")
		file, err := ini.Load(propertiesPath)
		if err != nil {
			if exec != UnknownExecutable {
				logger.Warn(errors.New("this server is missing a server.properties"))
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
	},
)

var savePath = tools.Memoize(
	func() string {
		serverProperties := serverProperties()
		if serverProperties == nil {
			return ""
		}
		levelName := serverProperties["level-name"]
		return path.Join(workPath(), levelName)
	},
)

var installedPackages = tools.Memoize(
	func() (mods []types.Package) {
		paths := modPaths()
		for _, modPath := range paths {
			jarFiles, err := findJar(modPath)
			if err != nil {
				logger.Warn(err)
				logger.Info("cannot read the mod directory")
				continue
			}
			for _, jarPath := range jarFiles {
				analyzed := detector.Packages(jarPath)
				if analyzed != nil {
					mods = append(mods, analyzed...)
				}
			}
		}

		env := getEnvironment()
		if env.Mcdr != nil {
			for _, dir := range env.Mcdr.PluginDirectories {
				pluginFiles, err := findFileWithExt(dir, ".pyz", ".mcdr")
				if err != nil {
					logger.Warn(err)
					logger.Info("cannot read the MCDR plugin directory")
					continue
				}
				for _, pluginFile := range pluginFiles {
					analyzed := detector.McdrPlugin(pluginFile)
					if analyzed != nil {
						mods = append(mods, analyzed...)
					}
				}
			}
		}

		sort.Slice(
			mods,
			func(i, j int) bool { return mods[i].Id.Name < mods[j].Id.Name },
		)
		return mods
	},
)
