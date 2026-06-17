package neoforge

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/providers/modloader"
	"github.com/mclucy/lucy/upstream/providers/mojang"
	"github.com/mclucy/lucy/workspace"
)

var (
	mavenBaseURL = "https://maven.neoforged.net/releases/net/neoforged/neoforge"
	metadataURL  = "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml"
	docsURL      = "https://docs.neoforged.net/user/docs/server/"
)

type provider struct{}

var Provider provider

func (provider) Id() types.SourceId {
	return types.SourceNeoForge
}

type mavenMetadata struct {
	Versioning struct {
		Latest   string   `xml:"latest"`
		Release  string   `xml:"release"`
		Versions []string `xml:"versions>version"`
	} `xml:"versioning"`
}

func (p provider) ResolveVersionSelector(id types.VersionedPackageRef) (
	types.VersionedPackageRef,
	error,
) {
	gameVersion, err := minecraftVersionForInstall()
	if err != nil {
		return id, err
	}

	neoForgeVersion, err := neoForgeVersionFromPackageRef(id, gameVersion)
	if err != nil {
		return id, err
	}

	return types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Platform: types.PlatformNeoforge,
			Name:     "neoforge",
		},
		Version: types.BareVersion(neoForgeVersion),
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
			"unknown minecraft version, cannot infer NeoForge bootstrap artifact; see %s",
			docsURL,
		)
	}

	if err := modloader.CheckJavaAvailability(); err != nil {
		return err
	}

	if err := mojang.EnsureEULAAccepted(workPath); err != nil {
		return err
	}

	neoForgeVersion, err := neoForgeVersionFromPackageRef(id, gameVersion)
	if err != nil {
		return err
	}
	id.Version = types.BareVersion(neoForgeVersion)

	fileURL := installerURL(neoForgeVersion)

	if err := modloader.RunInstaller(
		id,
		fileURL,
		workPath,
		"NeoForge",
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

func neoForgeVersionFromPackageRef(
	p types.VersionedPackageRef,
	gameVersion types.BareVersion,
) (string, error) {
	if p.Version != types.VersionLatest &&
		p.Version != types.VersionCompatible &&
		p.Version != types.VersionAny &&
		p.Version != types.VersionUnknown {
		return p.Version.String(), nil
	}
	return fetchLatestVersion(gameVersion)
}

func guardServerTopology() error {
	serverPlatform := workspace.ServerInfo().Runtime.DerivedModLoader()

	switch serverPlatform {
	case types.PlatformFabric, types.PlatformForge, types.PlatformNeoforge:
		return fmt.Errorf(
			"found an existing server platform %s, installation of NeoForge aborted",
			serverPlatform.Title(),
		)
	}
	return nil
}

func fetchLatestVersion(gameVersion types.BareVersion) (string, error) {
	res, err := http.Get(metadataURL)
	if err != nil {
		return "", fmt.Errorf("fetch NeoForge metadata failed: %w", err)
	}
	defer func() { _ = res.Body.Close() }()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf(
			"fetch NeoForge metadata failed: status %d",
			res.StatusCode,
		)
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return "", fmt.Errorf("read NeoForge metadata failed: %w", err)
	}

	var meta mavenMetadata
	if err := xml.Unmarshal(body, &meta); err != nil {
		return "", fmt.Errorf("parse NeoForge metadata failed: %w", err)
	}

	mcStr := gameVersion.String()
	parts := strings.SplitN(mcStr, ".", 3)
	if len(parts) < 2 {
		return "", fmt.Errorf(
			"cannot derive NeoForge version prefix from Minecraft version %s",
			gameVersion,
		)
	}
	neoPrefix := strings.Join(parts[1:], ".") + "."

	versions := meta.Versioning.Versions
	for i := len(versions) - 1; i >= 0; i-- {
		v := versions[i]
		if strings.HasPrefix(v, neoPrefix) {
			return v, nil
		}
	}

	return "", fmt.Errorf(
		"no NeoForge version found for Minecraft %s (looked for prefix %s); see %s",
		gameVersion,
		neoPrefix,
		docsURL,
	)
}

func installerURL(neoForgeVersion string) string {
	return fmt.Sprintf(
		"%s/%s/neoforge-%s-installer.jar",
		mavenBaseURL,
		neoForgeVersion,
		neoForgeVersion,
	)
}

func verifyInstallation(workPath string) error {
	launchScripts := []string{"run.sh", "run.bat"}
	for _, script := range launchScripts {
		if _, err := os.Stat(filepath.Join(workPath, script)); err == nil {
			return nil
		}
	}

	neoLibPath := filepath.Join(workPath, "libraries", "net", "neoforged")
	if _, err := os.Stat(neoLibPath); err == nil {
		return nil
	}

	return errors.New(
		"NeoForge installation verification failed: no artifacts found " +
			"(expected run.sh/run.bat or libraries/net/neoforged/)",
	)
}

func promptSelectMinecraftVersion() (version string) {
	manifest, err := mojang.FetchVersionManifest()
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
	options := huh.NewOptions(gameVersions...)
	err = huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("No current Minecraft installation found.").
				Description("Do you want to install NeoForge with its latest supported Minecraft version?").
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
				Title("Select a Minecraft version for NeoForge").
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
