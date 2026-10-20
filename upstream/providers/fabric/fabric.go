package fabric

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

const metaBaseURL = "https://meta.fabricmc.net"

type provider struct{}

var Provider provider

func (provider) Provider() types.Provider {
	return types.ProviderFabric
}

type installerVersion struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

type loaderVersionEntry struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

func (p provider) ResolveVersionSelector(
	_ upstream.LocalContext,
	id types.VersionedPackageRef,
) (types.VersionedPackageRef, error) {
	loaderVersion, err := loaderVersion(id.Version)
	if err != nil {
		return id, err
	}
	return types.VersionedPackageRef{
		Ecosystem: types.EcosystemFabric,
		Name:      "fabric",
		Version:   types.Version(loaderVersion),
	}, nil
}

func (p provider) Fetch(
	local upstream.LocalContext,
	id types.VersionedPackageRef,
) (types.ResolvedPackage, error) {
	var gameVersionID string
	switch local.Ecosystem {
	case types.EcosystemVanilla:
		gameVersionID = local.MinecraftVersion.String()
	case types.EcosystemUnspecified:
		gameVersionID = promptSelectMinecraftVersion()
	default:
		return types.ResolvedPackage{}, fmt.Errorf(
			"unsupported server platform %s for fabric bootstrap",
			local.Ecosystem.Title(),
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
		FileURL: url,
		Filename: fmt.Sprintf(
			"fabric-server-mc%s-loader%s-launcher%s.jar",
			gameVersionID,
			loaderVer,
			installerVer,
		),
	}, nil
}

func loaderVersion(selector types.Version) (string, error) {
	if selector == types.VersionUnknown {
		return "", errors.New("unknown game version, cannot resolve fabric loader version")
	}

	versions, err := fetchLoaderVersions()
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", errors.New("no fabric loader versions available")
	}

	switch selector {
	case types.VersionStable:
		for _, v := range versions {
			if v.Stable {
				return v.Version, nil
			}
		}
		return "", errors.New("no stable fabric loader version available")
	case types.VersionBeta, types.VersionAny:
		return versions[0].Version, nil
	}

	for _, v := range versions {
		if v.Version == selector.String() {
			return v.Version, nil
		}
	}

	return "", fmt.Errorf(
		"fabric loader version %s not found",
		selector.String(),
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
