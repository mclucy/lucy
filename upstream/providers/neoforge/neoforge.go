package neoforge

import (
	"encoding/xml"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

var (
	mavenBaseURL = "https://maven.neoforged.net/releases/net/neoforged/neoforge"
	metadataURL  = "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml"
	docsURL      = "https://docs.neoforged.net/user/docs/server/"
)

type provider struct{}

var Provider provider

func (provider) Provider() catalog.Provider {
	return catalog.ProviderNeoForge
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
	id catalog.VersionedRef,
) (catalog.VersionedRef, error) {
	gameVersion, err := minecraftVersionForInstall(local)
	if err != nil {
		return id, err
	}

	neoForgeVersion, err := neoForgeVersionFromPackageRef(id, gameVersion)
	if err != nil {
		return id, err
	}

	return catalog.VersionedRef{
		Ecosystem: catalog.EcosystemNeoForge,
		Name:      "neoforge",
		Version:   versionpkg.Version(neoForgeVersion),
	}, nil
}

func (p provider) Fetch(
	local upstream.LocalContext,
	id catalog.VersionedRef,
) (upstream.ResolvedPackage, error) {
	gameVersion, err := minecraftVersionForInstall(local)
	if err != nil {
		return upstream.ResolvedPackage{}, err
	}

	neoForgeVersion, err := neoForgeVersionFromPackageRef(id, gameVersion)
	if err != nil {
		return upstream.ResolvedPackage{}, err
	}

	fileURL := installerURL(neoForgeVersion)

	return upstream.ResolvedPackage{
		FileURL: fileURL,
		Filename: fmt.Sprintf(
			"neoforge-%s-installer.jar",
			neoForgeVersion,
		),
	}, nil
}

func minecraftVersionForInstall(local upstream.LocalContext) (versionpkg.Version, error) {
	if !local.HasMinecraftVersion() {
		return "", errors.New(
			"neoforge bootstrap requires a concrete Minecraft version",
		)
	}
	return local.MinecraftVersion, nil
}

func neoForgeVersionFromPackageRef(
	p catalog.VersionedRef,
	gameVersion versionpkg.Version,
) (string, error) {
	if p.Version != versionpkg.Beta &&
		p.Version != versionpkg.Stable &&
		p.Version != versionpkg.Any &&
		p.Version != versionpkg.Unknown {
		return p.Version.String(), nil
	}
	return fetchVersion(gameVersion, p.Version)
}

// fetchVersion selects the newest NeoForge version for the Minecraft
// version. stable excludes prerelease builds; beta accepts any build; any
// prefers a release and falls back to a prerelease.
func fetchVersion(
	gameVersion versionpkg.Version,
	selector versionpkg.Version,
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
	case versionpkg.Stable:
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
	case versionpkg.Beta:
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
