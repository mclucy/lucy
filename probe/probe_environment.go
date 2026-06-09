package probe

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/logger"
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
		logger.Warn(err)
		return
	}
	defer func(configFile io.ReadCloser) {
		err := configFile.Close()
		if err != nil {
			logger.Warn(err)
		}
	}(configFile)

	configData, err := io.ReadAll(configFile)
	if err != nil {
		logger.Warn(err)
		return
	}

	config := &exttype.FileMcdrConfig{}
	if err := yaml.Unmarshal(configData, config); err != nil {
		logger.Warn(err)
		return
	}

	bytes, err := exec.Command("mcdreforged", "--version").Output()
	if err != nil {
		logger.ReportWarn(
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
