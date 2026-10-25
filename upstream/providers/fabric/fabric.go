package fabric

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"time"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
)

const metaBaseURL = "https://meta.fabricmc.net"

type provider struct{}

var Provider provider

func (provider) Provider() catalog.Provider {
	return catalog.ProviderFabric
}

type installerVersion struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

type loaderVersionEntry struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

type gameVersionEntry struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

func (p provider) ResolveVersionSelector(
	_ upstream.LocalContext,
	id catalog.VersionedRef,
) (catalog.VersionedRef, error) {
	loaderVersion, err := loaderVersion(id.Version)
	if err != nil {
		return id, err
	}
	return catalog.VersionedRef{
		Ecosystem: catalog.EcosystemFabric,
		Name:      "fabric",
		Version:   version.Version(loaderVersion),
	}, nil
}

func (p provider) Fetch(
	local upstream.LocalContext,
	id catalog.VersionedRef,
) (upstream.ResolvedPackage, error) {
	if !local.HasMinecraftVersion() {
		return upstream.ResolvedPackage{}, errors.New(
			"fabric bootstrap requires a concrete Minecraft version",
		)
	}
	gameVersionID := local.MinecraftVersion.String()

	loaderVer, err := loaderVersion(id.Version)
	if err != nil {
		return upstream.ResolvedPackage{}, fmt.Errorf(
			"resolve fabric loader version failed: %w",
			err,
		)
	}

	installerVer, err := latestInstallerVersion()
	if err != nil {
		return upstream.ResolvedPackage{}, fmt.Errorf(
			"resolve fabric installer version failed: %w",
			err,
		)
	}

	url := fmt.Sprintf(
		"https://meta.fabricmc.net/v2/versions/loader/%s/%s/%s/server/jar",
		gameVersionID, loaderVer, installerVer,
	)

	return upstream.ResolvedPackage{
		FileURL: url,
		Filename: fmt.Sprintf(
			"fabric-server-mc%s-loader%s-launcher%s.jar",
			gameVersionID,
			loaderVer,
			installerVer,
		),
	}, nil
}

// LatestGameVersion reports the newest Minecraft release the loader publishes
// builds for. Fabric tracks Minecraft releases, so the entry is normally the
// current release.
func LatestGameVersion() (version.Version, error) {
	var versions []gameVersionEntry
	if err := fetchVersionsMeta("game", &versions); err != nil {
		return "", err
	}
	for _, entry := range versions {
		if entry.Stable {
			return version.Version(entry.Version), nil
		}
	}
	return "", errors.New("no stable Minecraft version in fabric metadata")
}

func loaderVersion(selector version.Version) (string, error) {
	if selector == version.Unknown {
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
	case version.Stable:
		for _, v := range versions {
			if v.Stable {
				return v.Version, nil
			}
		}
		return "", errors.New("no stable fabric loader version available")
	case version.Beta, version.Any:
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
