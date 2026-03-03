package install

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/mojang"
	"github.com/mclucy/lucy/util"
)

const minecraftEULAURL = "https://aka.ms/MinecraftEULA"

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

	if err := ensureMinecraftEULAAccepted(workPath); err != nil {
		return err
	}

	serverJar, data, err := util.DownloadFile(detail.Downloads.Server.Url, workPath)
	if err != nil {
		return fmt.Errorf("download minecraft server jar failed: %w", err)
	}
	defer func() { _ = serverJar.Close() }()

	if err := verifyMojangDownloadSha1(data, detail.Downloads.Server.Sha1); err != nil {
		return err
	}
	if err := addExecutePermission(serverJar); err != nil {
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

func ensureMinecraftEULAAccepted(workPath string) error {
	if hasAcceptedEULA(workPath) {
		return nil
	}

	accepted := false
	err := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Minecraft EULA consent required").
				Description(
					"To install and run the official server, you must agree to Mojang EULA: " + minecraftEULAURL,
				).
				Affirmative("I Agree").
				Negative("Cancel").
				Value(&accepted),
		),
	).Run()
	if err != nil {
		return fmt.Errorf(
			"unable to confirm EULA acceptance interactively after reviewing %s: %w",
			minecraftEULAURL,
			err,
		)
	}

	if !accepted {
		return fmt.Errorf(
			"minecraft server installation aborted: EULA was not accepted (%s)",
			minecraftEULAURL,
		)
	}

	return writeMinecraftEULAFile(workPath)
}

func hasAcceptedEULA(workPath string) bool {
	data, err := os.ReadFile(path.Join(workPath, "eula.txt"))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "eula=true")
}

func writeMinecraftEULAFile(workPath string) error {
	content := strings.Join(
		[]string{
			"# By changing the setting below to TRUE you are indicating your agreement to the Minecraft EULA.",
			"# " + minecraftEULAURL,
			"eula=true",
			"",
		},
		"\n",
	)
	err := os.WriteFile(path.Join(workPath, "eula.txt"), []byte(content), 0o644)
	if err != nil {
		return fmt.Errorf("write eula.txt failed: %w", err)
	}
	return nil
}

func addExecutePermission(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("read server jar file mode failed: %w", err)
	}

	mode := info.Mode()
	if mode&0o111 == 0o111 {
		return nil
	}

	if err := file.Chmod(mode | 0o111); err != nil {
		return fmt.Errorf("set execute permission on server jar failed: %w", err)
	}

	return nil
}
