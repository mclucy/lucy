package install

import (
	"errors"
	"fmt"

	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/util"
)

// installModLoaderPackage is a unified function to handle the installation of mods
// since most mod loaders has the same mod loading process
func installModLoaderPackage(p types.Package, platform types.Platform) error {
	if p.Id.Platform != platform {
		return fmt.Errorf("unsupported platform: %s", p.Id.Platform)
	}
	if p.Remote == nil {
		return errors.New("package remote data is missing")
	}
	serverInfo := probe.ServerInfo()
	if len(serverInfo.ModPath) == 0 {
		return errors.New("mod directory not found")
	}

	_, _, err := util.DownloadFile(
		p.Remote.FileUrl,
		serverInfo.ModPath[0],
	)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	return nil
}
