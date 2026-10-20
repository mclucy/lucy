package neoforge

import (
	"encoding/xml"
	"errors"
	"fmt"
	"slices"
	"strings"

	"charm.land/huh/v2"
	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/providers/mojang"
)

var (
	mavenBaseURL = "https://maven.neoforged.net/releases/net/neoforged/neoforge"
	metadataURL  = "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml"
	docsURL      = "https://docs.neoforged.net/user/docs/server/"
)

type provider struct{}

var Provider provider

func (provider) Provider() types.Provider {
	return types.ProviderNeoForge
}

type mavenMetadata struct {
	Versioning struct {
		Latest   string   `xml:"latest"`
		Release  string   `xml:"release"`
		Versions []string `xml:"versions>version"`
	} `xml:"versioning"`
}

func (p provider) ResolveVersionSelector(
	local upstream.LocalContext,
	id types.VersionedPackageRef,
) (types.VersionedPackageRef, error) {
	gameVersion, err := minecraftVersionForInstall(local)
	if err != nil {
		return id, err
	}

	neoForgeVersion, err := neoForgeVersionFromPackageRef(id, gameVersion)
	if err != nil {
		return id, err
	}

	return types.VersionedPackageRef{
		Ecosystem: types.EcosystemNeoForge,
		Name:      "neoforge",
		Version:   types.Version(neoForgeVersion),
	}, nil
}

func (p provider) Fetch(
	local upstream.LocalContext,
	id types.VersionedPackageRef,
) (types.ResolvedPackage, error) {
	gameVersion, err := minecraftVersionForInstall(local)
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
		FileURL: fileURL,
		Filename: fmt.Sprintf(
			"neoforge-%s-installer.jar",
			neoForgeVersion,
		),
	}, nil
}

func minecraftVersionForInstall(local upstream.LocalContext) (types.Version, error) {
	switch local.Ecosystem {
	case types.EcosystemVanilla:
		return local.MinecraftVersion, nil
	case types.EcosystemUnspecified:
		selectedVersion := promptSelectMinecraftVersion()
		if selectedVersion == "none" || selectedVersion == "error" {
			return "", errors.New("minecraft version selection cancelled or failed")
		}
		return types.Version(selectedVersion), nil
	default:
		return local.MinecraftVersion, nil
	}
}

func neoForgeVersionFromPackageRef(
	p types.VersionedPackageRef,
	gameVersion types.Version,
) (string, error) {
	if p.Version != types.VersionBeta &&
		p.Version != types.VersionStable &&
		p.Version != types.VersionAny &&
		p.Version != types.VersionUnknown {
		return p.Version.String(), nil
	}
	return fetchVersion(gameVersion, p.Version)
}

// fetchVersion selects the newest NeoForge version for the Minecraft
// version. stable excludes prerelease builds; beta accepts any build; any
// prefers a release and falls back to a prerelease.
func fetchVersion(
	gameVersion types.Version,
	selector types.Version,
) (string, error) {
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

	matching := make([]string, 0, len(meta.Versioning.Versions))
	for _, v := range meta.Versioning.Versions {
		if strings.HasPrefix(v, neoPrefix) {
			matching = append(matching, v)
		}
	}
	if len(matching) == 0 {
		return "", fmt.Errorf(
			"no NeoForge version found for Minecraft %s (looked for prefix %s); see %s",
			gameVersion,
			neoPrefix,
			docsURL,
		)
	}

	switch selector {
	case types.VersionStable:
		for _, v := range slices.Backward(matching) {
			if !isPrereleaseVersion(v) {
				return v, nil
			}
		}
		return "", fmt.Errorf(
			"no stable NeoForge release found for Minecraft %s; see %s",
			gameVersion,
			docsURL,
		)
	case types.VersionBeta:
		return matching[len(matching)-1], nil
	default:
		for _, v := range slices.Backward(matching) {
			if !isPrereleaseVersion(v) {
				return v, nil
			}
		}
		return matching[len(matching)-1], nil
	}
}

func isPrereleaseVersion(version string) bool {
	lower := strings.ToLower(version)
	for _, marker := range []string{
		"-beta", "-alpha", "-pre", "-rc", "-snapshot",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
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
			gameVersions = append(gameVersions, manifest.Versions[i].ID)
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
