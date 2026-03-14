package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/probe"
	tuiprogress "github.com/mclucy/lucy/tui/progress"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/util"
)

func getForgeVersionFromPackageId(p types.PackageId, gameVersion types.RawVersion) (string, error) {
	if p.Version != types.VersionLatest && p.Version != types.VersionCompatible && p.Version != types.VersionAny && p.Version != types.VersionUnknown {
		return p.Version.String(), nil
	}
	return fetchForgeVersion(gameVersion)
}

func checkJavaAvailability() error {
	_, err := exec.LookPath("java")
	if err != nil {
		return errors.New("java not found in PATH, Forge requires Java to install")
	}
	return nil
}

var (
	forgeDocsURL       = "https://files.minecraftforge.net/"
	forgePromotionsURL = "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json"
	forgeMavenBaseURL  = "https://maven.minecraftforge.net/net/minecraftforge/forge"

	// Forge/NeoForge installation differences (official docs):
	// 1) Artifact naming:
	//    Forge: forge-{mc_version}-{forge_version}-installer.jar
	//    NeoForge: neoforge-{version}-installer.jar
	// 2) Version metadata source:
	//    Forge: promotions_slim.json on files.minecraftforge.net
	//    NeoForge: release index from maven.neoforged.net
	// 3) Installation command:
	//    Both use: java -jar <installer>.jar --installServer
	forgeNeoForgeDiffDocURL = "https://docs.neoforged.net/user/docs/server"
)

type forgePromotions struct {
	Promos map[string]string `json:"promos"`
}

func init() {
	registerInstaller(types.PlatformForge, installForgeMod)
}

func installForgeMod(p types.Package) error {
	return installModLoaderPackage(p, types.PlatformForge)
}

func guardServerTopologyForForgePlatform() error {
	serverInfo := probe.ServerInfo()
	serverPlatform := serverInfo.Executable.DerivedModLoader()

	switch serverPlatform {
	case types.PlatformFabric, types.PlatformForge, types.PlatformNeoforge:
		return fmt.Errorf(
			"found an existing server platform %s, installation of forge aborted",
			serverPlatform.Title(),
		)
	}
	return nil
}

func promptSelectMinecraftVersionForForge() (version string) {
	manifest, err := fetchMojangVersionManifest()
	if err != nil || len(manifest.Versions) == 0 {
		return "error"
	}

	gameVersions := make([]string, 0, 20)
	for i := 0; i < len(manifest.Versions) && len(gameVersions) < 20; i++ {
		if manifest.Versions[i].Type == "release" {
			gameVersions = append(gameVersions, manifest.Versions[i].Id)
		}
	}

	var installLatest bool
	options := huh.NewOptions[string](gameVersions...)
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("No current Minecraft installation found.").
				Description("Do you want to install forge with its latest supported Minecraft version?").
				Affirmative("Yes, proceed").
				Negative("No, select a game version").
				Value(&installLatest),
		),
	).Run()
	if err != nil {
		return "none"
	}
	if installLatest {
		return gameVersions[0]
	}
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a Minecraft installation").
				Options(options...).
				Value(&version),
		).WithHide(installLatest),
	).Run()
	if err != nil {
		return "none"
	}
	return
}

func verifyForgeInstallation(workPath string) error {
	// Check for modern Forge (1.17+): libraries/ dir + launch script
	librariesPath := filepath.Join(workPath, "libraries")
	if _, err := os.Stat(librariesPath); err == nil {
		// libraries/ exists, check for launch scripts
		launchScripts := []string{"run.sh", "run.bat", "unix_args.txt", "win_args.txt"}
		for _, script := range launchScripts {
			if _, err := os.Stat(filepath.Join(workPath, script)); err == nil {
				return nil // Modern Forge verified
			}
		}
	}

	// Check for legacy Forge: forge-*-universal.jar or forge-*.jar
	entries, err := os.ReadDir(workPath)
	if err != nil {
		return fmt.Errorf("verify forge installation failed: cannot read work directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.Contains(name, "forge-") && strings.HasSuffix(name, ".jar") {
			return nil // Legacy Forge verified
		}
	}

	return errors.New("forge installation verification failed: no artifacts found (expected libraries/ with launch scripts or forge-*.jar)")
}

func installForge(p types.PackageId) error {
	if err := guardServerTopologyForForgePlatform(); err != nil {
		return err
	}

	serverInfo := probe.ServerInfo()
	if serverInfo.WorkPath == "" {
		return errors.New("server working directory not found")
	}

	var gameVersion types.RawVersion
	switch serverInfo.Executable.DerivedModLoader() {
	case types.PlatformVanilla:
		gameVersion = serverInfo.Executable.GameVersion
	case types.PlatformNone:
		selectedVersion := promptSelectMinecraftVersionForForge()
		if selectedVersion == "none" || selectedVersion == "error" {
			return errors.New("minecraft version selection cancelled or failed")
		}
		gameVersion = types.RawVersion(selectedVersion)
	}

	if gameVersion == types.VersionUnknown {
		return fmt.Errorf(
			"unknown minecraft version, cannot infer forge bootstrap artifact; see %s",
			forgeDocsURL,
		)
	}

	if err := checkJavaAvailability(); err != nil {
		return err
	}

	forgeVersion, err := getForgeVersionFromPackageId(p, gameVersion)
	if err != nil {
		return err
	}

	fileURL := resolveForgeInstallerURL(gameVersion, forgeVersion)

	tracker := tuiprogress.NewTracker("forge")

	var result *util.DownloadResult
	errCh := make(chan error, 1)
	go func() {
		defer tracker.Close()
		var err error
		result, err = util.CachedDownload(
			fileURL,
			serverInfo.WorkPath,
			util.DownloadOptions{
				Kind:               cache.KindArtifact,
				WrapReader:         tracker.ProxyReader,
				OnCacheHit:         tracker.CacheHit,
				OnResolvedFilename: func(title string) { tracker.SetTitle(title) },
			},
		)
		errCh <- err
	}()

	runErr := tracker.Run()
	dlErr := <-errCh
	if runErr != nil {
		logger.ShowError(fmt.Errorf("progress renderer failed: %w", runErr))
	}
	if dlErr != nil {
		return fmt.Errorf("download failed: %w", dlErr)
	}

	if result != nil {
		defer func() { _ = result.File.Close() }()
	}

	if err := ensureMinecraftEULAAccepted(serverInfo.WorkPath); err != nil {
		return err
	}

	if err := runForgeInstaller(result.File.Name(), serverInfo.WorkPath); err != nil {
		return err
	}

	if err := verifyForgeInstallation(serverInfo.WorkPath); err != nil {
		return err
	}

	probe.Rebuild()

	return nil
}

func fetchForgeVersion(gameVersion types.RawVersion) (string, error) {
	res, err := http.Get(forgePromotionsURL)
	if err != nil {
		return "", fmt.Errorf("fetch forge promotions failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf(
			"fetch forge promotions failed: status %d",
			res.StatusCode,
		)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("read forge promotions failed: %w", err)
	}

	var data forgePromotions
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("parse forge promotions failed: %w", err)
	}
	if len(data.Promos) == 0 {
		return "", fmt.Errorf("forge promotions is empty; see %s", forgeDocsURL)
	}

	keyBase := gameVersion.String()
	if v := data.Promos[keyBase+"-recommended"]; v != "" {
		return v, nil
	}
	if v := data.Promos[keyBase+"-latest"]; v != "" {
		return v, nil
	}

	return "", fmt.Errorf(
		"no forge version found for minecraft %s in promotions data; see %s (Forge) and %s (NeoForge comparison)",
		gameVersion,
		forgeDocsURL,
		forgeNeoForgeDiffDocURL,
	)
}

func resolveForgeInstallerURL(
	gameVersion types.RawVersion,
	forgeVersion string,
) string {
	combinedVersion := fmt.Sprintf("%s-%s", gameVersion.String(), forgeVersion)
	escaped := url.PathEscape(combinedVersion)
	return fmt.Sprintf(
		"%s/%s/forge-%s-installer.jar",
		forgeMavenBaseURL,
		escaped,
		escaped,
	)
}

func runForgeInstaller(installerPath string, workPath string) error {
	cmd := exec.Command("java", "-jar", installerPath, "--installServer")
	cmd.Dir = workPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		out := strings.TrimSpace(string(output))
		if out == "" {
			return fmt.Errorf("run forge installer failed: %w", err)
		}
		return fmt.Errorf("run forge installer failed: %w: %s", err, out)
	}
	return nil
}
