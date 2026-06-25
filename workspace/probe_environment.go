package workspace

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
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
	configPath := path.Join(dir, mcdrConfigFileName)

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

	configData, err := io.ReadAll(configFile)
	if err != nil {
		log.Warn(err)
		return
	}

	config := &fileschema.FileMcdrConfig{}
	if err := yaml.Unmarshal(configData, config); err != nil {
		log.Warn(err)
		return
	}

	bytes, err := exec.Command("mcdreforged", "--version").Output()
	if err != nil {
		log.ReportWarn(
			fmt.Errorf(
				"cannot execute mcdr, it is in your $PATH?: %w",
				err,
			),
		)
	}

	version := types.BareVersion(strings.Split(string(bytes), " ")[1])
	env.Mcdr = &types.McdrEnv{
		Version: version,
		Config:  config,
	}
}
