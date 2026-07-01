package fabric

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
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
			Eco:  types.EcoFabric,
			Name: "fabric",
		},
		Version: types.BareVersion(loaderVersion),
	}, nil
}

func (p provider) Fetch(id types.VersionedPackageRef) (
	types.ResolvedPackage,
	error,
) {
	ws := workspace.New()
	serverPlatform := ws.DerivedModLoader()

	var gameVersionID string
	switch serverPlatform {
	case types.EcoVanilla:
		gameVersionID = string(ws.Server.GameVersion)
	case types.EcoUnspecified:
		gameVersionID = promptSelectMinecraftVersion()
	default:
		return types.ResolvedPackage{}, fmt.Errorf(
			"unsupported server platform %s for fabric bootstrap",
			serverPlatform.Title(),
		)
	}

	loaderVer, err := loaderVersion(id.Version)
	if err != nil {
		return types.ResolvedPackage{}, fmt.Errorf(
			"resolve fabric loader version failed: %w",
			err,
		)
	}

	installerVer, err := latestInstallerVersion()
	if err != nil {
		return types.ResolvedPackage{}, fmt.Errorf(
			"resolve fabric installer version failed: %w",
			err,
		)
	}

	url := fmt.Sprintf(
		"https://meta.fabricmc.net/v2/versions/loader/%s/%s/%s/server/jar",
		gameVersionID, loaderVer, installerVer,
	)

	return types.ResolvedPackage{
		FileUrl: url,
		Filename: fmt.Sprintf(
			"fabric-server-mc%s-loader%s-launcher%s.jar",
			gameVersionID,
			loaderVer,
			installerVer,
		),
	}, nil
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
	data, err := cache.CachedGetBytes(
		apiEndpoint,
		cache.BytesRequestOptions{
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
