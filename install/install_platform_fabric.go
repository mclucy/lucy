package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/charmbracelet/huh"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/util"
)

func getFabricLoaderVersion(loaderVersion types.RawVersion) (string, error) {
	if loaderVersion == types.VersionUnknown {
		return "", errors.New("unknown game version, cannot resolve fabric loader version")
	}

	versions, err := fetchFabricLoaderVersions()
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

func getFabricGameVersion(gameVersion types.RawVersion) (string, error) {
	if gameVersion == types.VersionUnknown {
		return "", errors.New("unknown game version, cannot resolve fabric game version")
	}

	versions, err := fetchFabricGameVersions()
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

func getLatestFabricInstallerVersion() (string, error) {
	versions, err := fetchFabricInstallerVersions()
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", errors.New("no fabric installer versions found")
	}
	return versions[0].Version, nil
}

func fetchFabricLoaderVersions() (
	loaderVersions []fabricLoaderVersionEntry,
	err error,
) {
	err = fetchFabricVersionsMeta("loader", &loaderVersions)
	return
}

func fetchFabricGameVersions() (
	gameVersions []fabricInstallerVersion,
	err error,
) {
	err = fetchFabricVersionsMeta("game", &gameVersions)
	return
}

func fetchFabricInstallerVersions() (
	installerVersions []fabricInstallerVersion,
	err error,
) {
	err = fetchFabricVersionsMeta("installer", &installerVersions)
	return
}

func fetchFabricVersionsMeta(endpoint string, target any) (err error) {
	apiEndpoint := fabricMetaBaseURL + "/v2/versions/" + endpoint
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

func promptOverrideVanillaWithFabric() (override bool, deleteVanilla bool) {
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

func promptSelectMinecraftVersionForFabric() (version string) {
	versions, err := fetchFabricGameVersions()
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
				Value(&version),
		).WithHide(installLatest),
	).Run()
	if err != nil {
		return "none"
	}
	return
}
