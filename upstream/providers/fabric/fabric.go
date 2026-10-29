package fabric

import (
	"context"
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

// Provider resolves Fabric loader, installer, and game metadata over the shared
// network cache.
type Provider struct {
	network *cache.Client
}

// New constructs a Fabric installer provider. network supplies the cached HTTP
// transport; construction performs no requests.
func New(network *cache.Client) *Provider {
	return &Provider{network: network}
}

func (p *Provider) Provider() catalog.Provider {
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

func (p *Provider) ResolveVersionSelector(
	ctx context.Context,
	_ upstream.LocalContext,
	id catalog.VersionedRef,
) (catalog.VersionedRef, error) {
	loaderVer, err := p.loaderVersion(ctx, id.Version)
	if err != nil {
		return id, err
	}
	return catalog.VersionedRef{
		Ecosystem: catalog.EcosystemFabric,
		Name:      "fabric",
		Version:   version.Version(loaderVer),
	}, nil
}

func (p *Provider) Fetch(
	ctx context.Context,
	local upstream.LocalContext,
	id catalog.VersionedRef,
) (upstream.ResolvedPackage, error) {
	if !local.HasMinecraftVersion() {
		return upstream.ResolvedPackage{}, errors.New(
			"fabric bootstrap requires a concrete Minecraft version",
		)
	}
	gameVersionID := local.MinecraftVersion.String()

	loaderVer, err := p.loaderVersion(ctx, id.Version)
	if err != nil {
		return upstream.ResolvedPackage{}, fmt.Errorf(
			"resolve fabric loader version failed: %w",
			err,
		)
	}

	installerVer, err := p.latestInstallerVersion(ctx)
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
func (p *Provider) LatestGameVersion(ctx context.Context) (version.Version, error) {
	var versions []gameVersionEntry
	if err := p.fetchVersionsMeta(ctx, "game", &versions); err != nil {
		return "", err
	}
	for _, entry := range versions {
		if entry.Stable {
			return version.Version(entry.Version), nil
		}
	}
	return "", errors.New("no stable Minecraft version in fabric metadata")
}

func (p *Provider) loaderVersion(
	ctx context.Context,
	selector version.Version,
) (string, error) {
	if selector == version.Unknown {
		return "", errors.New("unknown game version, cannot resolve fabric loader version")
	}

	versions, err := p.fetchLoaderVersions(ctx)
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

func (p *Provider) latestInstallerVersion(ctx context.Context) (string, error) {
	versions, err := p.fetchInstallerVersions(ctx)
	if err != nil {
		return "", err
	}
	if len(versions) == 0 {
		return "", errors.New("no fabric installer versions found")
	}
	return versions[0].Version, nil
}

func (p *Provider) fetchLoaderVersions(ctx context.Context) (
	loaderVersions []loaderVersionEntry,
	err error,
) {
	err = p.fetchVersionsMeta(ctx, "loader", &loaderVersions)
	return
}

func (p *Provider) fetchInstallerVersions(ctx context.Context) (
	installerVersions []installerVersion,
	err error,
) {
	err = p.fetchVersionsMeta(ctx, "installer", &installerVersions)
	return
}

func (p *Provider) fetchVersionsMeta(
	ctx context.Context,
	endpoint string,
	target any,
) (err error) {
	apiEndpoint := metaBaseURL + "/v2/versions/" + endpoint
	data, err := p.network.CachedGetBytes(
		ctx,
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
