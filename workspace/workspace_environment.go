package workspace

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"gopkg.in/yaml.v3"
)

const mcdrConfigFileName = "config.yml"

func buildEnvironment() types.EnvironmentInfo {
	var env types.EnvironmentInfo
	detectMcdrEnvironment(".", &env)
	return env
}

func detectMcdrEnvironment(dir string, env *types.EnvironmentInfo) {
	configPath := filepath.Join(dir, mcdrConfigFileName)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return
	}

	configFile, err := os.Open(configPath)
	if err != nil {
		log.Warn(err)
		return
	}
	defer func(configFile io.ReadCloser) {
		err := configFile.Close()
		if err != nil {
			log.Warn(err)
		}
	}(configFile)

	config := &fileschema.FileMcdrConfig{}
	if err := yaml.NewDecoder(configFile).Decode(config); err != nil {
		log.Warn(err)
		return
	}

	for i, pluginDir := range config.PluginDirectories {
		if !filepath.IsAbs(pluginDir) {
			config.PluginDirectories[i] = filepath.Join(dir, pluginDir)
		}
	}

	version := types.VersionUnknown
	output, err := exec.Command("mcdreforged", "--version").Output()
	if err != nil {
		log.ReportWarn(
			fmt.Errorf(
				"cannot execute mcdr, is it in your $PATH?: %w",
				err,
			),
		)
	} else if fields := strings.Fields(string(output)); len(fields) > 1 {
		version = types.BareVersion(fields[1])
	} else {
		log.ReportWarn(fmt.Errorf("cannot parse mcdr version output"))
	}

	env.Mcdr = &types.McdrEnv{
		Version: version,
		Config:  config,
	}
}
