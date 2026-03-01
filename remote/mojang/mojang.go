package mojang

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/mclucy/lucy/exttype"
)

const VersionManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"

func getVersionManifest() (
	manifest *exttype.ApiMojangMinecraftVersionManifest,
	err error,
) {
	manifest = &exttype.ApiMojangMinecraftVersionManifest{}

	// TODO: Add cache mechanism if http call is too slow or fails
	resp, err := http.Get(VersionManifestURL)
	if err != nil {
		return nil, err
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	err = json.Unmarshal(data, manifest)
	if err != nil {
		return nil, err
	}

	return manifest, nil
}
