package neoforge

import (
	"encoding/xml"
	"errors"
	"fmt"
	"strings"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/types"
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
			Eco:  types.EcoNeoforge,
			Name: "neoforge",
		},
		Version: types.BareVersion(neoForgeVersion),
	}, nil
}

func (p provider) Fetch(id types.VersionedPackageRef) (
	types.ResolvedPackage,
	error,
) {
	gameVersion, err := minecraftVersionForInstall()
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	if gameVersion == types.VersionUnknown {
		return types.ResolvedPackage{}, fmt.Errorf(
			"unknown minecraft version, cannot infer NeoForge bootstrap artifact; see %s",
			docsURL,
		)
	}

	neoForgeVersion, err := neoForgeVersionFromPackageRef(id, gameVersion)
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	fileURL := installerURL(neoForgeVersion)

	return types.ResolvedPackage{
		FileUrl: fileURL,
		Filename: fmt.Sprintf(
			"neoforge-%s-installer.jar",
			neoForgeVersion,
		),
	}, nil
}

func minecraftVersionForInstall() (types.BareVersion, error) {
	ws := workspace.New()
	switch ws.DerivedModLoader() {
	case types.EcoVanilla:
		return ws.Runtime.GameVersion, nil
	case types.EcoBare:
		selectedVersion := promptSelectMinecraftVersion()
		if selectedVersion == "none" || selectedVersion == "error" {
			return "", errors.New("minecraft version selection cancelled or failed")
		}
		return types.BareVersion(selectedVersion), nil
	default:
		return ws.Runtime.GameVersion, nil
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

func fetchLatestVersion(gameVersion types.BareVersion) (string, error) {
	body, err := cache.CachedGetBytes(
		metadataURL,
		cache.BytesRequestOptions{Kind: cache.KindMetadata},
	)
	if err != nil {
		return "", fmt.Errorf("fetch NeoForge metadata failed: %w", err)
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
