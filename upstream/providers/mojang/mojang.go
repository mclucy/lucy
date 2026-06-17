package mojang

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"
	"time"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/tui/progress"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/util"
)

const (
	VersionManifestURL = "https://piston-meta.mojang.com/mc/game/version_manifest_v2.json"
	minecraftEULAURL   = "https://aka.ms/MinecraftEULA"
)

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
	*exttype.ApiMojangMinecraftVersionManifest,
	error,
) {
	data, err := util.CachedGetBytes(
		VersionManifestURL,
		util.BytesRequestOptions{Kind: cache.KindMetadata},
	)
	if err != nil {
		return nil, fmt.Errorf("fetch mojang version manifest failed: %w", err)
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

func ResolveVersionEntry(
	manifest *exttype.ApiMojangMinecraftVersionManifest,
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

func (p provider) InstallPlatform(
	id types.VersionedPackageRef,
	serverDir string,
) error {
	if probe.ServerInfo().Runtime.DerivedModLoader() != types.PlatformNone {
		return errors.New("a server is already installed")
	}

	manifest, err := FetchVersionManifest()
	if err != nil {
		return err
	}

	versionID, versionURL, err := ResolveVersionEntry(manifest, id.Version)
	if err != nil {
		return err
	}

	detail, err := fetchVersionDetail(versionURL)
	if err != nil {
		return err
	}

	if detail.Downloads.Server == nil {
		return fmt.Errorf(
			"minecraft version %s does not provide a dedicated server jar",
			versionID,
		)
	}

	workPath := serverDir
	if workPath == "" {
		workPath = probe.ServerInfo().Root
	}
	if workPath == "" {
		workPath = "."
	}

	if err := EnsureEULAAccepted(workPath); err != nil {
		return err
	}

	serverJar, err := downloadServerJar(
		detail.Downloads.Server.Url,
		detail.Downloads.Server.Sha1,
		workPath,
	)
	if err != nil {
		return fmt.Errorf("download minecraft server jar failed: %w", err)
	}
	defer func() { _ = serverJar.Close() }()

	if err := addExecutePermission(serverJar); err != nil {
		return err
	}

	probe.Rebuild()
	return nil
}

func fetchVersionDetail(versionURL string) (*versionDetail, error) {
	data, err := util.CachedGetBytes(
		versionURL,
		util.BytesRequestOptions{
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

func downloadServerJar(
	url string,
	expectedSha1 string,
	dir string,
) (*os.File, error) {
	tracker := progress.NewTracker("Downloading server")
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = progress.WaitForShutdown(ctx)
	}()
	defer tracker.Close()

	result, err := util.CachedDownload(
		url, dir, util.DownloadOptions{
			Kind:          cache.KindArtifact,
			ExpectedHash:  expectedSha1,
			HashAlgorithm: cache.HashSHA1,
			WrapReader:    tracker.ProxyReader,
			OnResolvedFilename: func(name string) {
				tracker.SetTitle(name)
			},
			OnCacheHit: func() {
				tracker.Complete("cache hit")
				time.Sleep(500 * time.Millisecond)
			},
		},
	)
	if err != nil {
		return nil, err
	}

	return result.File, nil
}

func EnsureEULAAccepted(workPath string) error {
	if hasAcceptedEULA(workPath) {
		return nil
	}

	accepted := false
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Minecraft EULA consent required").
				Description("To install and run the official server, you must agree to Mojang EULA: " + minecraftEULAURL).
				Affirmative("I Agree").
				Negative("Cancel").
				Value(&accepted),
		),
	)
	err := form.Run()
	if err != nil {
		return fmt.Errorf(
			"unable to confirm EULA acceptance interactively after reviewing %s: %w",
			minecraftEULAURL, err,
		)
	}

	if !accepted {
		return fmt.Errorf(
			"minecraft server installation aborted: EULA was not accepted (%s)",
			minecraftEULAURL,
		)
	}

	return writeEULAFile(workPath)
}

func hasAcceptedEULA(workPath string) bool {
	data, err := os.ReadFile(path.Join(workPath, "eula.txt"))
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(string(data)), "eula=true")
}

func writeEULAFile(workPath string) error {
	content := strings.Join(
		[]string{
			"# By changing the setting below to TRUE you are indicating your agreement to the Minecraft EULA.",
			"# " + minecraftEULAURL,
			"eula=true",
			"",
		},
		"\n",
	)
	if _, err := os.Stat(path.Join(workPath)); os.IsNotExist(err) {
		err = os.MkdirAll(path.Join(workPath), 0o755)
		if err != nil {
			return err
		}
	}
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
		return fmt.Errorf(
			"set execute permission on server jar failed: %w",
			err,
		)
	}

	return nil
}
