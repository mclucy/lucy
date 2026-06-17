package fabric

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/tui/progress"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/util"
)

const metaBaseURL = "https://meta.fabricmc.net"

type provider struct{}

var Provider provider

func (provider) Id() types.SourceId {
	return types.SourceFabric
}

type installerVersion struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

type loaderVersionEntry struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

func (p provider) ResolveVersionSelector(id types.VersionedPackageRef) (
	types.VersionedPackageRef,
	error,
) {
	loaderVersion, err := loaderVersion(id.Version)
	if err != nil {
		return id, err
	}
	return types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Platform: types.PlatformFabric,
			Name:     "fabric",
		},
		Version: types.BareVersion(loaderVersion),
	}, nil
}

func (p provider) InstallPlatform(
	id types.VersionedPackageRef,
	serverDir string,
) error {
	serverInfo := probe.ServerInfo()
	serverPlatform := serverInfo.Runtime.DerivedModLoader()

	switch serverPlatform {
	case types.PlatformUnknown:
		return errors.New("unknown mod loader, cannot infer fabric bootstrap artifact")
	case types.PlatformFabric:
		return errors.New("fabric server already detected, installation aborted")
	case types.PlatformForge:
		return errors.New("Forge server detected, cannot install Fabric bootstrap")
	case types.PlatformNeoforge:
		return errors.New("NeoForge server detected, cannot install Fabric bootstrap")
	case types.PlatformVanilla:
		override, deleteVanilla := promptOverrideVanilla()
		if !override {
			return errors.New("installation aborted by user")
		}
		return installWithOverride(id, serverDir, deleteVanilla)
	case types.PlatformNone:
	default:
		return fmt.Errorf(
			"unsupported server platform %s for fabric installation",
			serverPlatform.Title(),
		)
	}
	return installWithOverride(id, serverDir, false)
}

func installWithOverride(
	p types.VersionedPackageRef,
	serverDir string,
	deleteVanilla bool,
) error {
	serverInfo := probe.ServerInfo()

	workPath := serverDir
	if workPath == "" {
		workPath = serverInfo.Root
	}
	if workPath == "" {
		workPath = "."
	}

	var gameVersionID string
	switch serverInfo.Runtime.DerivedModLoader() {
	case types.PlatformVanilla:
		gameVersionID = string(serverInfo.Runtime.GameVersion)
	case types.PlatformNone:
		gameVersionID = promptSelectMinecraftVersion()
	}

	loaderVersion, err := loaderVersion(p.Version)
	if err != nil {
		return fmt.Errorf("resolve fabric loader version failed: %w", err)
	}
	if gameVersionID == "" {
		gameVersionID, err = gameVersion(serverInfo.Runtime.GameVersion)
		if err != nil {
			return fmt.Errorf("cannot install fabric for game version: %w", err)
		}
	}
	installerVersion, err := latestInstallerVersion()
	if err != nil {
		return fmt.Errorf("cannot get fabric loader version: %w", err)
	}

	artifactURL := fmt.Sprintf(
		"https://meta.fabricmc.net/v2/versions/loader/%s/%s/%s/server/jar",
		gameVersionID, loaderVersion, installerVersion,
	)

	tracker := progress.NewTracker("fabric")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = progress.WaitForShutdown(ctx)
	}()
	defer tracker.Close()

	result, err := util.CachedDownload(
		artifactURL,
		workPath,
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
		err = os.Remove(serverInfo.Runtime.PrimaryEntrance)
		if err != nil {
			return fmt.Errorf("delete vanilla server failed: %w", err)
		}
	}
	probe.Rebuild()

	return nil
}

func loaderVersion(loaderVersion types.BareVersion) (string, error) {
	if loaderVersion == types.VersionUnknown {
		return "", errors.New("unknown game version, cannot resolve fabric loader version")
	}

	versions, err := fetchLoaderVersions()
	if err != nil {
		return "", err
	}

	if loaderVersion == types.VersionLatest || loaderVersion == types.VersionCompatible || loaderVersion == types.VersionAny {
		if len(versions) == 0 {
			return "", errors.New("no fabric loader versions available")
		}
		return versions[0].Version, nil
	}

	for _, v := range versions {
		if v.Version == loaderVersion.String() {
			return v.Version, nil
		}
	}

	return "", fmt.Errorf(
		"fabric loader version %s not found",
		loaderVersion.String(),
	)
}

func gameVersion(gameVersion types.BareVersion) (string, error) {
	if gameVersion == types.VersionUnknown {
		return "", errors.New("unknown game version, cannot resolve fabric game version")
	}

	versions, err := fetchGameVersions()
	if err != nil {
		return "", err
	}

	if gameVersion == types.VersionLatest || gameVersion == types.VersionCompatible || gameVersion == types.VersionAny {
		if len(versions) == 0 {
			return "", errors.New("no fabric game versions available")
		}
		return versions[0].Version, nil
	}

	for _, v := range versions {
		if v.Version == gameVersion.String() {
			return v.Version, nil
		}
	}

	return "", fmt.Errorf(
		"fabric game version %s not found",
		gameVersion.String(),
	)
}

func latestInstallerVersion() (string, error) {
	versions, err := fetchInstallerVersions()
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", errors.New("no fabric installer versions found")
	}
	return versions[0].Version, nil
}

func fetchLoaderVersions() (
	loaderVersions []loaderVersionEntry,
	err error,
) {
	err = fetchVersionsMeta("loader", &loaderVersions)
	return
}

func fetchGameVersions() (
	gameVersions []installerVersion,
	err error,
) {
	err = fetchVersionsMeta("game", &gameVersions)
	return
}

func fetchInstallerVersions() (
	installerVersions []installerVersion,
	err error,
) {
	err = fetchVersionsMeta("installer", &installerVersions)
	return
}

func fetchVersionsMeta(endpoint string, target any) (err error) {
	apiEndpoint := metaBaseURL + "/v2/versions/" + endpoint
	data, err := util.CachedGetBytes(
		apiEndpoint,
		util.BytesRequestOptions{
			Kind: cache.KindMetadata,
			TTL:  3 * 24 * time.Hour,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"fetch fabric %s versions meta failed: %w",
			endpoint, err,
		)
	}

	err = json.Unmarshal(data, target)
	if err != nil {
		return fmt.Errorf(
			"parse fabric %s versions meta failed: %w",
			endpoint, err,
		)
	}

	return
}

func promptOverrideVanilla() (override bool, deleteVanilla bool) {
	path := probe.ServerInfo().Runtime.PrimaryEntrance
	version := probe.ServerInfo().Runtime.GameVersion
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Vanilla server detected, override it with a corresponding fabric server?").
				Description(
					fmt.Sprintf(
						"Found server at %s, with game version %s",
						path, version,
					),
				).
				Value(&override),
		),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Delete vanilla server after fabric installation?").
				Description(fmt.Sprintf("Will delete %s", path)).
				Value(&deleteVanilla),
		).WithHideFunc(func() bool { return !override }),
	)
	_ = form.Run()
	return
}

func promptSelectMinecraftVersion() (version string) {
	versions, err := fetchGameVersions()
	if err != nil || len(versions) == 0 {
		return "error"
	}

	gameVersions := make([]string, len(versions))
	for i, v := range versions {
		gameVersions[i] = v.Version
	}

	var installLatest bool
	options := huh.NewOptions[string](gameVersions...)
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("No current Minecraft installation found.").
				Description("Do you want to install fabric with its latest supported Minecraft version?").
				Affirmative("Yes, proceed").
				Negative("No, select a game version").
				Value(&installLatest),
		),
	).Run()
	if err != nil {
		return "none"
	}
	if installLatest {
		return gameVersions[0]
	}
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a Minecraft installation").
				Options(options...).
				Filtering(true).
				Height(10).
				Value(&version),
		).WithHide(installLatest),
	).Run()
	if err != nil {
		return "none"
	}
	return
}
