package install

import (
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
)

func init() {
	registerInstaller(types.PlatformNeoforge, installNeoForgeMod)
}

func installNeoForgeMod(p types.Package) error {
	return installModLoaderPackage(p, types.PlatformNeoforge)
}

// guardServerTopologyForNeoForgePlatform returns an error if an incompatible
// mod loader is already installed.
func guardServerTopologyForNeoForgePlatform() error {
	serverInfo := probe.ServerInfo()
	serverPlatform := serverInfo.Runtime.DerivedModLoader()

	switch serverPlatform {
	case types.PlatformFabric, types.PlatformForge, types.PlatformNeoforge:
		return fmt.Errorf(
			"found an existing server platform %s, installation of NeoForge aborted",
			serverPlatform.Title(),
		)
	}
	return nil
}

var (
	neoForgeMavenBaseURL = "https://maven.neoforged.net/releases/net/neoforged/neoforge"
	neoForgeMetadataURL  = "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml"
	neoForgeDocsURL      = "https://docs.neoforged.net/user/docs/server/"
)

type neoForgeMavenMetadata struct {
	Versioning struct {
		Latest   string   `xml:"latest"`
		Release  string   `xml:"release"`
		Versions []string `xml:"versions>version"`
	} `xml:"versioning"`
}

func installNeoForge(id types.PackageId) error {
	if err := guardServerTopologyForNeoForgePlatform(); err != nil {
		return err
	}

	serverInfo := probe.ServerInfo()
	if serverInfo.WorkPath == "" {
		return errors.New("server working directory not found")
	}

	var gameVersion types.RawVersion
	switch serverInfo.Runtime.DerivedModLoader() {
	case types.PlatformVanilla:
		gameVersion = serverInfo.Runtime.GameVersion
	case types.PlatformNone:
		selectedVersion := promptSelectMinecraftVersionForNeoForge()
		if selectedVersion == "none" || selectedVersion == "error" {
			return errors.New("minecraft version selection cancelled or failed")
		}
		gameVersion = types.RawVersion(selectedVersion)
	}

	if gameVersion == types.VersionUnknown {
		return fmt.Errorf(
			"unknown minecraft version, cannot infer NeoForge bootstrap artifact; see %s",
			neoForgeDocsURL,
		)
	}

	if err := checkJavaAvailability(); err != nil {
		return err
	}

	if err := ensureMinecraftEULAAccepted(serverInfo.WorkPath); err != nil {
		return err
	}

	neoForgeVersion, err := getNeoForgeVersionFromPackageId(id, gameVersion)
	if err != nil {
		return err
	}
	id.Version = types.RawVersion(neoForgeVersion)

	fileURL := resolveNeoForgeInstallerURL(neoForgeVersion)

	if err := runModLoaderInstaller(
		id,
		fileURL,
		serverInfo.WorkPath,
		"NeoForge",
	); err != nil {
		return err
	}

	return verifyNeoForgeInstallation(serverInfo.WorkPath)
}

// getNeoForgeVersionFromPackageId resolves the NeoForge version to install.
// If the version is explicit, it is returned as-is.
// Otherwise, the latest compatible version for the given Minecraft game version is fetched.
func getNeoForgeVersionFromPackageId(
	p types.PackageId,
	gameVersion types.RawVersion,
) (string, error) {
	if p.Version != types.VersionLatest &&
		p.Version != types.VersionCompatible &&
		p.Version != types.VersionAny &&
		p.Version != types.VersionUnknown {
		return p.Version.String(), nil
	}
	return fetchLatestNeoForgeVersion(gameVersion)
}

// fetchLatestNeoForgeVersion fetches the latest NeoForge version compatible with
// the given Minecraft game version from the NeoForged Maven metadata.
//
// NeoForge version scheme: MAJOR.MINOR.PATCH where MAJOR = MC minor version,
// MINOR = MC patch version. E.g. NeoForge 21.4.x is for Minecraft 1.21.4.
func fetchLatestNeoForgeVersion(gameVersion types.RawVersion) (string, error) {
	res, err := http.Get(neoForgeMetadataURL)
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

	var meta neoForgeMavenMetadata
	if err := xml.Unmarshal(body, &meta); err != nil {
		return "", fmt.Errorf("parse NeoForge metadata failed: %w", err)
	}

	// NeoForge version prefix derived from MC version: "1.21.4" -> "21.4."
	mcStr := gameVersion.String()
	parts := strings.SplitN(mcStr, ".", 3)
	if len(parts) < 2 {
		return "", fmt.Errorf(
			"cannot derive NeoForge version prefix from Minecraft version %s",
			gameVersion,
		)
	}
	// Drop the leading "1." from MC version to get NeoForge major.minor prefix
	neoPrefix := strings.Join(parts[1:], ".") + "."

	// Walk versions in reverse to find the latest matching version
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
		neoForgeDocsURL,
	)
}

// resolveNeoForgeInstallerURL builds the full Maven URL for a NeoForge installer JAR.
// Pattern: {mavenBase}/{version}/neoforge-{version}-installer.jar
func resolveNeoForgeInstallerURL(neoForgeVersion string) string {
	return fmt.Sprintf(
		"%s/%s/neoforge-%s-installer.jar",
		neoForgeMavenBaseURL,
		neoForgeVersion,
		neoForgeVersion,
	)
}

// verifyNeoForgeInstallation checks that the NeoForge installer produced the
// expected server artifacts in workPath.
//
// NeoForge generates: run.sh / run.bat, user_jvm_args.txt, libraries/net/neoforged/
func verifyNeoForgeInstallation(workPath string) error {
	// Check for launch scripts
	launchScripts := []string{"run.sh", "run.bat"}
	for _, script := range launchScripts {
		if _, err := os.Stat(filepath.Join(workPath, script)); err == nil {
			return nil
		}
	}

	// Fallback: check for NeoForge libraries directory
	neoLibPath := filepath.Join(workPath, "libraries", "net", "neoforged")
	if _, err := os.Stat(neoLibPath); err == nil {
		return nil
	}

	return errors.New(
		"NeoForge installation verification failed: no artifacts found " +
			"(expected run.sh/run.bat or libraries/net/neoforged/)",
	)
}

// promptSelectMinecraftVersionForNeoForge prompts the user to select a Minecraft
// version when no server executable is present.
func promptSelectMinecraftVersionForNeoForge() (version string) {
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
				Value(&version),
		).WithHide(installLatest),
	).Run()
	if err != nil {
		return "none"
	}
	return
}
