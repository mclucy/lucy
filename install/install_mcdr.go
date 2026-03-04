package install

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/probe"
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

	result, err := util.CachedDownload(p.Remote.FileUrl, pluginDirectories[0], util.DownloadOptions{
		Kind:          cache.KindArtifact,
		Filename:      p.Remote.Filename,
		ExpectedHash:  p.Remote.Hash,
		HashAlgorithm: cache.ParseHashAlgorithm(p.Remote.HashAlgorithm),
	})
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer result.File.Close()

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
