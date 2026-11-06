package forge

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
)

const (
	docsURL               = "https://files.minecraftforge.net/"
	promotionsURL         = "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json"
	mavenBaseURL          = "https://maven.minecraftforge.net/net/minecraftforge/forge"
	neoForgeComparisonURL = "https://docs.neoforged.net/user/docs/server"
)

// Provider resolves Forge promotions and installer artifacts over the shared
// network cache.
type Provider struct {
	network *cache.Client
}

// New constructs a Forge installer provider. network supplies the cached HTTP
// transport; construction performs no requests.
func New(network *cache.Client) *Provider {
	return &Provider{network: network}
}

func (p *Provider) Provider() catalog.Provider {
	return catalog.ProviderForge
}

type promotions struct {
	Promos map[string]string `json:"promos"`
}

// LatestMinecraftVersion reports the newest Minecraft version the promotions
// list a build for. Forge publishes release builds only.
func (p *Provider) LatestMinecraftVersion(ctx context.Context) (
	version.Version,
	error,
) {
	body, err := p.network.CachedGetBytes(
		ctx,
		promotionsURL,
		cache.BytesRequestOptions{Kind: cache.KindMetadata},
	)
	if err != nil {
		return "", fmt.Errorf("fetch forge promotions failed: %w", err)
	}
	var data promotions
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("parse forge promotions failed: %w", err)
	}
	versions := make([]version.Version, 0, len(data.Promos))
	for key := range data.Promos {
		minecraft, _, found := strings.Cut(key, "-")
		if found && minecraft != "" {
			versions = append(versions, version.Version(minecraft))
		}
	}
	return version.NewestMinecraft(versions)
}

func (p *Provider) ResolveVersionSelector(
	ctx context.Context,
	local upstream.LocalContext,
	id catalog.VersionedRef,
) (catalog.VersionedRef, error) {
	gameVersion, err := minecraftVersionForInstall(local)
	if err != nil {
		return id, err
	}

	forgeVersion, err := p.forgeVersionFromPackageRef(ctx, id, gameVersion)
	if err != nil {
		return id, err
	}

	return catalog.VersionedRef{
		Ecosystem: catalog.EcosystemForge,
		Name:      "forge",
		Version:   version.Version(forgeVersion),
	}, nil
}

func (p *Provider) Fetch(
	ctx context.Context,
	local upstream.LocalContext,
	id catalog.VersionedRef,
) (upstream.ResolvedPackage, error) {
	gameVersion, err := minecraftVersionForInstall(local)
	if err != nil {
		return upstream.ResolvedPackage{}, err
	}

	forgeVersion, err := p.forgeVersionFromPackageRef(ctx, id, gameVersion)
	if err != nil {
		return upstream.ResolvedPackage{}, err
	}

	fileURL := installerURL(gameVersion, forgeVersion)

	return upstream.ResolvedPackage{
		FileURL: fileURL,
		Filename: fmt.Sprintf(
			"forge-%s-%s-installer.jar",
			gameVersion.String(),
			forgeVersion,
		),
	}, nil
}

func minecraftVersionForInstall(local upstream.LocalContext) (version.Version, error) {
	if !local.HasMinecraftVersion() {
		return "", errors.New(
			"forge bootstrap requires a concrete Minecraft version",
		)
	}
	return local.MinecraftVersion, nil
}

func (p *Provider) forgeVersionFromPackageRef(
	ctx context.Context,
	ref catalog.VersionedRef,
	gameVersion version.Version,
) (string, error) {
	if ref.Version != version.Beta &&
		ref.Version != version.Stable &&
		ref.Version != version.Any &&
		ref.Version != version.Unknown {
		return ref.Version.String(), nil
	}
	return p.fetchVersion(ctx, gameVersion, ref.Version)
}

// fetchVersion selects a Forge version from the `promotions` data. stable
// accepts the recommended promotion only; beta accepts the latest promotion;
// any prefers the recommended release and falls back to the latest.
func (p *Provider) fetchVersion(
	ctx context.Context,
	gameVersion version.Version,
	selector version.Version,
) (string, error) {
	body, err := p.network.CachedGetBytes(
		ctx,
		promotionsURL,
		cache.BytesRequestOptions{Kind: cache.KindMetadata},
	)
	if err != nil {
		return "", fmt.Errorf("fetch forge promotions failed: %w", err)
	}

	var data promotions
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("parse forge promotions failed: %w", err)
	}
	if len(data.Promos) == 0 {
		return "", fmt.Errorf("forge promotions is empty; see %s", docsURL)
	}

	keyBase := gameVersion.String()
	recommended := data.Promos[keyBase+"-recommended"]
	latest := data.Promos[keyBase+"-latest"]
	switch selector {
	case version.Stable:
		if recommended != "" {
			return recommended, nil
		}
	case version.Beta:
		if latest != "" {
			return latest, nil
		}
		if recommended != "" {
			return recommended, nil
		}
	default:
		if recommended != "" {
			return recommended, nil
		}
		if latest != "" {
			return latest, nil
		}
	}

	return "", fmt.Errorf(
		"no forge version found for minecraft %s in promotions data; see %s (Forge) and %s (NeoForge comparison)",
		gameVersion,
		docsURL,
		neoForgeComparisonURL,
	)
}

func installerURL(
	gameVersion version.Version,
	forgeVersion string,
) string {
	combinedVersion := fmt.Sprintf("%s-%s", gameVersion.String(), forgeVersion)
	escaped := url.PathEscape(combinedVersion)
	return fmt.Sprintf(
		"%s/%s/forge-%s-installer.jar",
		mavenBaseURL,
		escaped,
		escaped,
	)
}
