package install

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/tui/progress"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/util"
)

var fabricMetaBaseURL = "https://meta.fabricmc.net"

// Docs: https://fabricmc.net/use/server/
// Fabric install bootstraps from the server launch jar and resolves versions via Fabric Meta.

type fabricInstallerVersion struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

type fabricLoaderVersionEntry struct {
	_       string `json:"separator"`
	_       int    `json:"build"`
	_       string `json:"maven"`
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

func init() {
	registerInstaller(types.PlatformFabric, installFabricMod)
}

func installFabric(p types.PackageId) error {
	return installFabricWithOverride(p, false)
}

func installFabricWithOverride(p types.PackageId, deleteVanilla bool) error {
	serverInfo := probe.ServerInfo()

	var gameVersion string
	switch serverInfo.Executable.DerivedModLoader() {
	case types.PlatformVanilla:
		gameVersion = string(serverInfo.Executable.GameVersion)
	case types.PlatformNone:
		gameVersion = promptSelectMinecraftVersionForFabric()
	}

	loaderVersion, err := getFabricLoaderVersion(p.Version)
	if err != nil {
		return fmt.Errorf("resolve fabric loader version failed: %w", err)
	}
	if gameVersion == "" {
		gameVersion, err = getFabricGameVersion(serverInfo.Executable.GameVersion)
		if err != nil {
			return fmt.Errorf("cannot install fabric for game version: %w", err)
		}
	}
	installerVersion, err := getLatestFabricInstallerVersion()
	if err != nil {
		return fmt.Errorf("cannot get fabric loader version: %w", err)
	}

	artifactUrl := fmt.Sprintf(
		"https://meta.fabricmc.net/v2/versions/loader/%s/%s/%s/server/jar",
		gameVersion, loaderVersion, installerVersion,
	)

	tracker := progress.NewTracker("fabric")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = progress.WaitForShutdown(ctx)
	}()
	defer tracker.Close()

	result, err := util.CachedDownload(
		artifactUrl,
		serverInfo.WorkPath,
		util.DownloadOptions{
			Kind:               cache.KindArtifact,
			WrapReader:         tracker.ProxyReader,
			OnCacheHit:         tracker.CacheHit,
			OnResolvedFilename: func(title string) { tracker.SetTitle(title) },
		},
	)

	if result != nil {
		tools.CloseReader(result.File, nil)
	}
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	if deleteVanilla {
		err = os.Remove(serverInfo.Executable.Path)
	}
	probe.Rebuild()

	return nil
}

func installFabricMod(p types.Package) error {
	return installModLoaderPackage(p, types.PlatformFabric)
}
