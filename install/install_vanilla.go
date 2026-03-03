package install

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/mojang"
	"github.com/mclucy/lucy/util"
)

type mojangVersionDetail struct {
	Downloads struct {
		Server *struct {
			Sha1 string `json:"sha1"`
			Url  string `json:"url"`
		} `json:"server"`
	} `json:"downloads"`
}

func installMinecraftServer(id types.PackageId) error {
	manifest, err := fetchMojangVersionManifest()
	if err != nil {
		return err
	}

	versionId, versionURL, err := resolveMinecraftVersionEntry(manifest, id.Version)
	if err != nil {
		return err
	}

	detail, err := fetchMojangVersionDetail(versionURL)
	if err != nil {
		return err
	}

	if detail.Downloads.Server == nil {
		return fmt.Errorf(
			"minecraft version %s does not provide a dedicated server jar",
			versionId,
		)
	}

	workPath := probe.ServerInfo().WorkPath
	if workPath == "" {
		workPath = "."
	}

	serverJar, data, err := util.DownloadFile(detail.Downloads.Server.Url, workPath)
	if err != nil {
		return fmt.Errorf("download minecraft server jar failed: %w", err)
	}
	defer func() { _ = serverJar.Close() }()

	if err := verifyMojangDownloadSha1(data, detail.Downloads.Server.Sha1); err != nil {
		return err
	}

	return nil
}

func fetchMojangVersionManifest() (
	*exttype.ApiMojangMinecraftVersionManifest,
	error,
) {
	resp, err := http.Get(mojang.VersionManifestURL)
	if err != nil {
		return nil, fmt.Errorf("fetch mojang version manifest failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"fetch mojang version manifest failed: status %d",
			resp.StatusCode,
		)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read mojang version manifest failed: %w", err)
	}

	manifest := &exttype.ApiMojangMinecraftVersionManifest{}
	if err := json.Unmarshal(data, manifest); err != nil {
		return nil, fmt.Errorf("parse mojang version manifest failed: %w", err)
	}

	if len(manifest.Versions) == 0 {
		return nil, errors.New("mojang version manifest has no versions")
	}

	return manifest, nil
}

func resolveMinecraftVersionEntry(
	manifest *exttype.ApiMojangMinecraftVersionManifest,
	targetVersion types.RawVersion,
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

func fetchMojangVersionDetail(versionURL string) (*mojangVersionDetail, error) {
	resp, err := http.Get(versionURL)
	if err != nil {
		return nil, fmt.Errorf("fetch minecraft version metadata failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf(
			"fetch minecraft version metadata failed: status %d",
			resp.StatusCode,
		)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read minecraft version metadata failed: %w", err)
	}

	detail := &mojangVersionDetail{}
	if err := json.Unmarshal(data, detail); err != nil {
		return nil, fmt.Errorf("parse minecraft version metadata failed: %w", err)
	}

	return detail, nil
}

func verifyMojangDownloadSha1(data []byte, expected string) error {
	if expected == "" {
		return nil
	}

	actual := sha1.Sum(data)
	actualHex := hex.EncodeToString(actual[:])
	if !strings.EqualFold(actualHex, expected) {
		return fmt.Errorf(
			"minecraft server jar sha1 mismatch: expected %s, got %s",
			expected,
			actualHex,
		)
	}

	return nil
}
