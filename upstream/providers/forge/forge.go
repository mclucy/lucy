package forge

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/providers/modloader"
	"github.com/mclucy/lucy/upstream/providers/mojang"
	"github.com/mclucy/lucy/workspace"
)

const (
	docsURL               = "https://files.minecraftforge.net/"
	promotionsURL         = "https://files.minecraftforge.net/net/minecraftforge/forge/promotions_slim.json"
	mavenBaseURL          = "https://maven.minecraftforge.net/net/minecraftforge/forge"
	neoForgeComparisonURL = "https://docs.neoforged.net/user/docs/server"
)

type provider struct{}

var Provider provider

func (provider) Id() types.SourceId {
	return types.SourceForge
}

type promotions struct {
	Promos map[string]string `json:"promos"`
}

func (p provider) ResolveVersionSelector(id types.VersionedPackageRef) (
	types.VersionedPackageRef,
	error,
) {
	gameVersion, err := minecraftVersionForInstall()
	if err != nil {
		return id, err
	}

	forgeVersion, err := forgeVersionFromPackageRef(id, gameVersion)
	if err != nil {
		return id, err
	}

	return types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Platform: types.PlatformForge,
			Name:     "forge",
		},
		Version: types.BareVersion(forgeVersion),
	}, nil
}

func (p provider) InstallPlatform(
	id types.VersionedPackageRef,
	serverDir string,
) error {
	if err := guardServerTopology(); err != nil {
		return err
	}

	serverInfo := workspace.ServerInfo()
	workPath := serverDir
	if workPath == "" {
		workPath = serverInfo.Root
	}
	if workPath == "" {
		return errors.New("server working directory not found")
	}

	gameVersion, err := minecraftVersionForInstall()
	if err != nil {
		return err
	}

	if gameVersion == types.VersionUnknown {
		return fmt.Errorf(
			"unknown minecraft version, cannot infer forge bootstrap artifact; see %s",
			docsURL,
		)
	}

	if err := modloader.CheckJavaAvailability(); err != nil {
		return err
	}

	if err := mojang.EnsureEULAAccepted(workPath); err != nil {
		return err
	}

	promptSupportProject()

	forgeVersion, err := forgeVersionFromPackageRef(id, gameVersion)
	if err != nil {
		return err
	}
	id.Version = types.BareVersion(forgeVersion)

	fileURL := installerURL(gameVersion, forgeVersion)

	if err := modloader.RunInstaller(
		id,
		fileURL,
		workPath,
		"Forge",
	); err != nil {
		return err
	}

	return verifyInstallation(workPath)
}

func minecraftVersionForInstall() (types.BareVersion, error) {
	serverInfo := workspace.ServerInfo()
	switch serverInfo.Runtime.DerivedModLoader() {
	case types.PlatformVanilla:
		return serverInfo.Runtime.GameVersion, nil
	case types.PlatformNone:
		selectedVersion := promptSelectMinecraftVersion()
		if selectedVersion == "none" || selectedVersion == "error" {
			return "", errors.New("minecraft version selection cancelled or failed")
		}
		return types.BareVersion(selectedVersion), nil
	default:
		return serverInfo.Runtime.GameVersion, nil
	}
}

func forgeVersionFromPackageRef(
	p types.VersionedPackageRef,
	gameVersion types.BareVersion,
) (string, error) {
	if p.Version != types.VersionLatest &&
		p.Version != types.VersionCompatible &&
		p.Version != types.VersionAny &&
		p.Version != types.VersionUnknown {
		return p.Version.String(), nil
	}
	return fetchVersion(gameVersion)
}

func guardServerTopology() error {
	serverPlatform := workspace.ServerInfo().Runtime.DerivedModLoader()

	switch serverPlatform {
	case types.PlatformFabric, types.PlatformForge, types.PlatformNeoforge:
		return fmt.Errorf(
			"found an existing server platform %s, installation of forge aborted",
			serverPlatform.Title(),
		)
	}
	return nil
}

func promptSupportProject() {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewNote().
				Title("Supporting the Forge project").
				Description(
					"The Forge project is sustained by ads on the download page. By automating " +
						"this process, we may reduce ad revenue that supports the project. If you find " +
						"Forge useful, please consider supporting the project by downloading manually " +
						"from their official site <https://files.minecraftforge.net>, or support them on " +
						"Patreon at <https://www.patreon.com/LexManos>",
				),
		),
	).WithWidth(80)
	_ = form.Run()
}

func promptSelectMinecraftVersion() (version string) {
	versions, err := fetchSupportedMinecraftVersions()
	if err != nil || len(versions) == 0 {
		return "error"
	}

	gameVersions := versions

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
		return gameVersions[len(gameVersions)-1]
	}
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select a Minecraft installation").
				Options(options...).
				Filtering(true).
				Height(10).
				Value(&version),
		).WithHide(installLatest),
	).Run()
	if err != nil {
		return "none"
	}
	return
}

func fetchSupportedMinecraftVersions() ([]string, error) {
	data, err := cache.CachedGetBytes(
		promotionsURL,
		cache.BytesRequestOptions{Kind: cache.KindMetadata},
	)
	if err != nil {
		return nil, fmt.Errorf("fetch forge promotions failed: %w", err)
	}

	versions, err := parseSupportedMinecraftVersions(data)
	if err != nil {
		return nil, err
	}
	if len(versions) == 0 {
		return nil, fmt.Errorf(
			"forge promotions is empty; see %s",
			docsURL,
		)
	}

	return versions, nil
}

func parseSupportedMinecraftVersions(data []byte) ([]string, error) {
	var payload struct {
		Promos json.RawMessage `json:"promos"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("parse forge promotions failed: %w", err)
	}

	dec := json.NewDecoder(bytes.NewReader(payload.Promos))
	tok, err := dec.Token()
	if err != nil {
		return nil, fmt.Errorf("parse forge promotions failed: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("parse forge promotions failed: promos is not an object")
	}

	seen := map[string]struct{}{}
	versions := make([]string, 0)
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, fmt.Errorf("parse forge promotions failed: %w", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("parse forge promotions failed: invalid promos key")
		}

		if _, err := dec.Token(); err != nil {
			return nil, fmt.Errorf("parse forge promotions failed: %w", err)
		}

		base, ok := strings.CutSuffix(key, "-recommended")
		if !ok {
			base, ok = strings.CutSuffix(key, "-latest")
		}
		if !ok {
			continue
		}
		if _, exists := seen[base]; exists {
			continue
		}
		if !strings.HasPrefix(base, "1.") {
			continue
		}
		seen[base] = struct{}{}
		versions = append(versions, base)
	}

	return versions, nil
}

func fetchVersion(gameVersion types.BareVersion) (string, error) {
	res, err := http.Get(promotionsURL)
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

	var data promotions
	if err := json.Unmarshal(body, &data); err != nil {
		return "", fmt.Errorf("parse forge promotions failed: %w", err)
	}
	if len(data.Promos) == 0 {
		return "", fmt.Errorf("forge promotions is empty; see %s", docsURL)
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
		docsURL,
		neoForgeComparisonURL,
	)
}

func installerURL(
	gameVersion types.BareVersion,
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

func verifyInstallation(workPath string) error {
	librariesPath := filepath.Join(workPath, "libraries")
	if _, err := os.Stat(librariesPath); err == nil {
		launchScripts := []string{
			"run.sh", "run.bat", "unix_args.txt", "win_args.txt",
		}
		for _, script := range launchScripts {
			if _, err := os.Stat(filepath.Join(workPath, script)); err == nil {
				return nil
			}
		}
	}

	entries, err := os.ReadDir(workPath)
	if err != nil {
		return fmt.Errorf(
			"verify forge installation failed: cannot read work directory: %w",
			err,
		)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if strings.Contains(name, "forge-") && strings.HasSuffix(name, ".jar") {
			return nil
		}
	}

	return errors.New("forge installation verification failed: no artifacts found (expected libraries/ with launch scripts or forge-*.jar)")
}
