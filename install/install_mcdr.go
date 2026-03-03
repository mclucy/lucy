package install

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/util"
)

func init() {
	registerInstaller(types.PlatformMCDR, installMcdrPlugin)
}

func installMcdrPlugin(p types.Package) error {
	if p.Id.Platform != types.PlatformMCDR {
		return fmt.Errorf("unsupported platform: %s", p.Id.Platform)
	}
	if p.Remote == nil {
		return errors.New("package remote data is missing")
	}

	serverInfo := probe.ServerInfo()
	if serverInfo.Environments.Mcdr == nil {
		return errors.New("mcdr not found")
	}
	pluginDirectories := serverInfo.Environments.Mcdr.Config.PluginDirectories
	if len(pluginDirectories) == 0 {
		return errors.New("mcdr plugin directory not found")
	}

	file, _, err := util.DownloadFile(p.Remote.FileUrl, pluginDirectories[0])
	tools.CloseReader(file, nil)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	return nil
}

func initMcdr() error {
	err := exec.Command(
		"mcdreforged",
		"--version",
	).Run() // check if mcdreforged is in PATH
	if err != nil {
		return err
	}

	// make subdir
	err = os.Mkdir("server", 0o755)
	if err != nil {
		return err
	}

	// move everything to subdir
	files, err := os.ReadDir(".")
	if err != nil {
		return err
	}
	for _, file := range files {
		if file.Name() == "server" {
			continue
		}
		err = os.Rename(file.Name(), "server/"+file.Name())
		if err != nil {
			return err
		}
	}

	// init mcdr
	err = exec.Command(
		"mcdreforged",
		"init",
	).Run()
	if err != nil {
		return err
	}

	// rebuild server info
	probe.Rebuild()

	return nil
}
