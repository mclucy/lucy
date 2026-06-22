package mojang

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/types"
)

const VersionManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

type provider struct{}

var Provider provider

func (provider) Id() types.SourceId {
	return types.SourceMojang
}

type versionDetail struct {
	Downloads struct {
		Server *struct {
			Sha1 string `json:"sha1"`
			Url  string `json:"url"`
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
	targetVersion types.BareVersion,
) (string, string, error) {
	selected := targetVersion.String()
	if targetVersion == "" || targetVersion.CanInfer() || targetVersion == types.VersionUnknown {
		selected = manifest.Latest.Release
	}

	if strings.EqualFold(selected, "snapshot") {
		selected = manifest.Latest.Snapshot
	}

	for i := range manifest.Versions {
		if manifest.Versions[i].Id == selected {
			return manifest.Versions[i].Id, manifest.Versions[i].Url, nil
		}
	}

	return "", "", fmt.Errorf(
		"minecraft version %s not found in mojang manifest",
		targetVersion.String(),
	)
}

func (p provider) ResolveVersionSelector(id types.VersionedPackageRef) (
	types.VersionedPackageRef,
	error,
) {
	manifest, err := FetchVersionManifest()
	if err != nil {
		return id, err
	}

	versionID, _, err := ResolveVersionEntry(manifest, id.Version)
	if err != nil {
		return id, err
	}

	return types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Platform: types.PlatformMinecraft,
			Name:     "minecraft",
		},
		Version: types.BareVersion(versionID),
	}, nil
}

func (p provider) Fetch(id types.VersionedPackageRef) (types.ResolvedPackage, error) {
	manifest, err := FetchVersionManifest()
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	versionID, versionURL, err := ResolveVersionEntry(manifest, id.Version)
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	detail, err := fetchVersionDetail(versionURL)
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	if detail.Downloads.Server == nil {
		return types.ResolvedPackage{}, fmt.Errorf(
			"minecraft version %s does not provide a dedicated server jar",
			versionID,
		)
	}

	return types.ResolvedPackage{
		FileUrl:       detail.Downloads.Server.Url,
		Filename:      "server.jar",
		Hash:          detail.Downloads.Server.Sha1,
		HashAlgorithm: "sha1",
	}, nil
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
