package workspace

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/mclucy/lucy/log"
	versionpkg "github.com/mclucy/lucy/version"
	"gopkg.in/yaml.v3"
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

	configFile, err := os.Open(configPath)
	if err != nil {
		warnErr := fmt.Errorf("cannot open %s: %w", configPath, err)
		log.Warn(warnErr)
		warnings = append(warnings, warnErr)
		return warnings
	}
	defer func(configFile io.ReadCloser) {
		err := configFile.Close()
		if err != nil {
			log.Warn(err)
		}
	}(configFile)

	config := &MCDRConfig{}
	if err := yaml.NewDecoder(configFile).Decode(config); err != nil {
		warnErr := fmt.Errorf("cannot decode %s: %w", configPath, err)
		log.Warn(warnErr)
		warnings = append(warnings, warnErr)
		return warnings
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
	output, err := exec.CommandContext(ctx, "mcdreforged", "--version").Output()
	if err != nil {
		warnErr := fmt.Errorf(
			"cannot execute mcdr, is it in your $PATH?: %w",
			err,
		)
		log.Warn(warnErr)
		warnings = append(warnings, warnErr)
	} else if fields := strings.Fields(string(output)); len(fields) > 1 {
		version = versionpkg.Version(fields[1])
	} else {
		warnErr := fmt.Errorf("cannot parse mcdr version output")
		log.Warn(warnErr)
		warnings = append(warnings, warnErr)
	}

	wrappers.MCDR = &MCDREnv{
		Version: version,
		Config:  config,
	}
	return warnings
}
