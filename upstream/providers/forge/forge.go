package forge

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

const (
	docsURL               = "https://files.minecraftforge.net/"
	promotionsURL         = "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json"
	mavenBaseURL          = "https://maven.minecraftforge.net/net/minecraftforge/forge"
	neoForgeComparisonURL = "https://docs.neoforged.net/user/docs/server"
)

type provider struct{}

var Provider provider

func (provider) Provider() types.Provider {
	return types.ProviderForge
}

type promotions struct {
	Promos map[string]string `json:"promos"`
}

func (p provider) ResolveVersionSelector(
	local upstream.LocalContext,
	id types.VersionedPackageRef,
) (types.VersionedPackageRef, error) {
	gameVersion, err := minecraftVersionForInstall(local)
	if err != nil {
		return id, err
	}

	forgeVersion, err := forgeVersionFromPackageRef(id, gameVersion)
	if err != nil {
		return id, err
	}

	return types.VersionedPackageRef{
		Ecosystem: types.EcosystemForge,
		Name:      "forge",
		Version:   types.Version(forgeVersion),
	}, nil
}

func (p provider) Fetch(
	local upstream.LocalContext,
	id types.VersionedPackageRef,
) (types.ResolvedPackage, error) {
	gameVersion, err := minecraftVersionForInstall(local)
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	forgeVersion, err := forgeVersionFromPackageRef(id, gameVersion)
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	fileURL := installerURL(gameVersion, forgeVersion)

	return types.ResolvedPackage{
		FileURL: fileURL,
		Filename: fmt.Sprintf(
			"forge-%s-%s-installer.jar",
			gameVersion.String(),
			forgeVersion,
		),
	}, nil
}

func minecraftVersionForInstall(local upstream.LocalContext) (types.Version, error) {
	if !local.HasMinecraftVersion() {
		return "", errors.New(
			"forge bootstrap requires a concrete Minecraft version",
		)
	}
	return local.MinecraftVersion, nil
}

func forgeVersionFromPackageRef(
	p types.VersionedPackageRef,
	gameVersion types.Version,
) (string, error) {
	if p.Version != types.VersionBeta &&
		p.Version != types.VersionStable &&
		p.Version != types.VersionAny &&
		p.Version != types.VersionUnknown {
		return p.Version.String(), nil
	}
	return fetchVersion(gameVersion, p.Version)
}

// fetchVersion selects a Forge version from the `promotions` data. stable
// accepts the recommended promotion only; beta accepts the latest promotion;
// any prefers the recommended release and falls back to the latest.
func fetchVersion(
	gameVersion types.Version,
	selector types.Version,
) (string, error) {
	body, err := cache.CachedGetBytes(
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
	case types.VersionStable:
		if recommended != "" {
			return recommended, nil
		}
	case types.VersionBeta:
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
	gameVersion types.Version,
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
