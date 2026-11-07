package workspace

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	versionpkg "github.com/mclucy/lucy/version"
)

const mcdrConfigFileName = "config.yml"

// detectWrappers reads the wrappers of a server directory and the
// recoverable warnings seen while reading them. These wrappers do not come
// from jars. An MCDR installation manages a server directory from outside
// the directory.
func detectWrappers(dir string) (WrapperInfo, []error) {
	var wrappers WrapperInfo
	warnings := detectMCDRWrapper(dir, &wrappers)
	return wrappers, warnings
}

// detectMCDRWrapper detects an MCDR managed directory and returns the MCDR
// version-detection warnings. The marker is the config.yml file. It records
// the wrapper version. The version comes from the mcdreforged command on
// PATH. Relative plugin directories resolve against dir.
//
// MCDR changes the server directory, so later consumers will not depend on
// pwd. Warnings are diagnostics for CLI presentation; the wrapper detection
// itself only logs them.
func detectMCDRWrapper(dir string, wrappers *WrapperInfo) []error {
	var warnings []error
	configPath := filepath.Join(dir, mcdrConfigFileName)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil
	}

	config, err := readMCDRConfig(configPath)
	if err != nil {
		return []error{err}
	}

	for i, pluginDir := range config.PluginDirectories {
		if !filepath.IsAbs(pluginDir) {
			config.PluginDirectories[i] = filepath.Join(dir, pluginDir)
		}
	}

	// A hung mcdreforged must not block workspace observation.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	version := versionpkg.Unknown
	// MCDReforged is provisioned by install, so absence from PATH is normal.
	if output, err := exec.CommandContext(ctx, "mcdreforged", "--version").Output(); err == nil {
		if fields := strings.Fields(string(output)); len(fields) > 1 {
			version = versionpkg.Version(fields[1])
		} else {
			warnErr := fmt.Errorf("cannot parse mcdr version output")
			warnings = append(warnings, warnErr)
		}
	}

	wrappers.MCDR = &MCDREnv{
		Version: version,
		Config:  config,
	}
	return warnings
}
