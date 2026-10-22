package mojang

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
)

const VersionManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

type provider struct{}

var Provider provider

func (provider) Provider() catalog.Provider {
	return catalog.ProviderMojang
}

type versionDetail struct {
	JavaVersion *struct {
		MajorVersion int `json:"majorVersion"`
	} `json:"javaVersion"`
	Downloads struct {
		Server *struct {
			Sha1 string `json:"sha1"`
			URL  string `json:"url"`
		} `json:"server"`
	} `json:"downloads"`
}

func FetchVersionManifest() (
	*fileschema.ApiMojangMinecraftVersionManifest,
	error,
) {
	data, err := cache.CachedGetBytes(
		VersionManifestURL,
		cache.BytesRequestOptions{Kind: cache.KindMetadata},
	)
	if err != nil {
		return nil, fmt.Errorf("fetch mojang version manifest failed: %w", err)
	}

	manifest := &fileschema.ApiMojangMinecraftVersionManifest{}
	if err := json.Unmarshal(data, manifest); err != nil {
		return nil, fmt.Errorf("parse mojang version manifest failed: %w", err)
	}

	if len(manifest.Versions) == 0 {
		return nil, errors.New("mojang version manifest has no versions")
	}

	return manifest, nil
}

func ResolveVersionEntry(
	manifest *fileschema.ApiMojangMinecraftVersionManifest,
	targetVersion version.Version,
) (string, string, error) {
	selected := targetVersion.String()
	if targetVersion == "" || targetVersion.CanInfer() || targetVersion == version.Unknown {
		selected = manifest.Latest.Release
	}

	if strings.EqualFold(selected, "snapshot") {
		selected = manifest.Latest.Snapshot
	}

	for i := range manifest.Versions {
		if manifest.Versions[i].ID == selected {
			return manifest.Versions[i].ID, manifest.Versions[i].URL, nil
		}
	}

	return "", "", fmt.Errorf(
		"minecraft version %s not found in mojang manifest",
		targetVersion.String(),
	)
}

func (p provider) ResolveVersionSelector(
	_ upstream.LocalContext,
	id catalog.VersionedRef,
) (catalog.VersionedRef, error) {
	manifest, err := FetchVersionManifest()
	if err != nil {
		return id, err
	}

	versionID, _, err := ResolveVersionEntry(manifest, id.Version)
	if err != nil {
		return id, err
	}

	return catalog.VersionedRef{
		Ecosystem: catalog.EcosystemMinecraft,
		Name:      "minecraft",
		Version:   version.Version(versionID),
	}, nil
}

func (p provider) Fetch(
	_ upstream.LocalContext,
	id catalog.VersionedRef,
) (upstream.ResolvedPackage, error) {
	manifest, err := FetchVersionManifest()
	if err != nil {
		return upstream.ResolvedPackage{}, err
	}

	versionID, versionURL, err := ResolveVersionEntry(manifest, id.Version)
	if err != nil {
		return upstream.ResolvedPackage{}, err
	}

	detail, err := fetchVersionDetail(versionURL)
	if err != nil {
		return upstream.ResolvedPackage{}, err
	}

	if detail.Downloads.Server == nil {
		return upstream.ResolvedPackage{}, fmt.Errorf(
			"minecraft version %s does not provide a dedicated server jar",
			versionID,
		)
	}

	return upstream.ResolvedPackage{
		FileURL:       detail.Downloads.Server.URL,
		Filename:      "server.jar",
		Hash:          detail.Downloads.Server.Sha1,
		HashAlgorithm: "sha1",
	}, nil
}

// legacyJavaAdvisory is the Java major version for versions whose metadata
// predates the javaVersion block.
const legacyJavaAdvisory = 8

// JavaAdvisory returns the Java major version Mojang's launcher ships for the
// Minecraft version. Lucy treats it as an advisory minimum.
//
// Versions from 13w24a through 1.6.4 carry no javaVersion block. That metadata
// field arrived later, and Mojang pairs the versions on both sides of the gap
// with JRE 8, so those versions fall back to 8.
func JavaAdvisory(minecraft version.Version) (int, error) {
	manifest, err := FetchVersionManifest()
	if err != nil {
		return 0, err
	}

	versionID, versionURL, err := ResolveVersionEntry(manifest, minecraft)
	if err != nil {
		return 0, err
	}

	detail, err := fetchVersionDetail(versionURL)
	if err != nil {
		return 0, err
	}

	if detail.JavaVersion == nil {
		return legacyJavaAdvisory, nil
	}

	if detail.JavaVersion.MajorVersion <= 0 {
		return 0, fmt.Errorf(
			"minecraft version %s declares no java major version",
			versionID,
		)
	}

	return detail.JavaVersion.MajorVersion, nil
}

func fetchVersionDetail(versionURL string) (*versionDetail, error) {
	data, err := cache.CachedGetBytes(
		versionURL,
		cache.BytesRequestOptions{
			Kind: cache.KindMetadata,
			TTL:  7 * 24 * time.Hour,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"fetch minecraft version metadata failed: %w",
			err,
		)
	}

	detail := &versionDetail{}
	if err := json.Unmarshal(data, detail); err != nil {
		return nil, fmt.Errorf(
			"parse minecraft version metadata failed: %w",
			err,
		)
	}

	return detail, nil
}
