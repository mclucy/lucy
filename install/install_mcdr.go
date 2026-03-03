package install

import (
	"errors"
	"fmt"

	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/util"
)

func init() {
	registerInstaller(types.Mcdr, installMcdrPlugin)
}

func installMcdrPlugin(p types.Package) error {
	if p.Id.Platform != types.Mcdr {
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

	_, _, err := util.DownloadFile(p.Remote.FileUrl, pluginDirectories[0])
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	return nil
}
