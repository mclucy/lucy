package neoforge

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"slices"
	"strconv"
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

// Provider resolves NeoForge Maven metadata and installer artifacts over the
// shared network cache.
type Provider struct {
	network *cache.Client
}

// New constructs a NeoForge installer provider. network supplies the cached
// HTTP transport; construction performs no requests.
func New(network *cache.Client) *Provider {
	return &Provider{network: network}
}

func (p *Provider) Provider() catalog.Provider {
	return catalog.ProviderNeoForge
}

type mavenMetadata struct {
	Versioning struct {
		Latest   string   `xml:"latest"`
		Release  string   `xml:"release"`
		Versions []string `xml:"versions>version"`
	} `xml:"versioning"`
}

// LatestMinecraftVersion reports the newest Minecraft version the metadata
// lists a build for. NeoForge encodes the Minecraft version in the loader
// version prefix.
func (p *Provider) LatestMinecraftVersion(ctx context.Context) (
	versionpkg.Version,
	error,
) {
	body, err := p.network.CachedGetBytes(
		ctx,
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
	versions := make([]versionpkg.Version, 0, len(meta.Versioning.Versions))
	for _, loader := range meta.Versioning.Versions {
		if minecraft, ok := minecraftVersionOf(loader); ok {
			versions = append(versions, minecraft)
		}
	}
	return versionpkg.NewestMinecraft(versions)
}

// neoForgePrefix names the loader version prefix for a Minecraft version. A
// 1.x release drops the leading 1 (1.21.4 → 21.4.), while a year-based
// release keeps its full version (26.3 → 26.3.).
func neoForgePrefix(gameVersion versionpkg.Version) (string, bool) {
	parts := strings.Split(gameVersion.String(), ".")
	if len(parts) < 2 {
		return "", false
	}
	year, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", false
	}
	if year >= 26 {
		return gameVersion.String() + ".", true
	}
	return strings.Join(parts[1:], ".") + ".", true
}

// minecraftVersionOf inverts the loader version prefix: 21.4.16 belongs to
// Minecraft 1.21.4, 21.1.66 to 1.21.1, and 26.3.0.98 to 26.3.
func minecraftVersionOf(loader string) (versionpkg.Version, bool) {
	base, _, _ := strings.Cut(loader, "-")
	parts := strings.Split(base, ".")
	if len(parts) < 3 {
		return "", false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", false
	}
	minor, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", false
	}
	if major >= 26 {
		patch, err := strconv.Atoi(parts[2])
		if err != nil {
			return "", false
		}
		if patch != 0 {
			return versionpkg.Version(fmt.Sprintf("%d.%d.%d", major, minor, patch)), true
		}
		return versionpkg.Version(fmt.Sprintf("%d.%d", major, minor)), true
	}
	if minor == 0 {
		return versionpkg.Version(fmt.Sprintf("1.%d", major)), true
	}
	return versionpkg.Version(fmt.Sprintf("1.%d.%d", major, minor)), true
}

func (p *Provider) ResolveVersionSelector(
	ctx context.Context,
	local upstream.LocalContext,
	id catalog.VersionedRef,
) (catalog.VersionedRef, error) {
	gameVersion, err := minecraftVersionForInstall(local)
	if err != nil {
		return id, err
	}

	neoForgeVersion, err := p.neoForgeVersionFromPackageRef(ctx, id, gameVersion)
	if err != nil {
		return id, err
	}

	return catalog.VersionedRef{
		Ecosystem: catalog.EcosystemNeoForge,
		Name:      "neoforge",
		Version:   versionpkg.Version(neoForgeVersion),
	}, nil
}

func (p *Provider) Fetch(
	ctx context.Context,
	local upstream.LocalContext,
	id catalog.VersionedRef,
) (upstream.ResolvedPackage, error) {
	gameVersion, err := minecraftVersionForInstall(local)
	if err != nil {
		return upstream.ResolvedPackage{}, err
	}

	neoForgeVersion, err := p.neoForgeVersionFromPackageRef(ctx, id, gameVersion)
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

func (p *Provider) neoForgeVersionFromPackageRef(
	ctx context.Context,
	ref catalog.VersionedRef,
	gameVersion versionpkg.Version,
) (string, error) {
	if ref.Version != versionpkg.Beta &&
		ref.Version != versionpkg.Stable &&
		ref.Version != versionpkg.Any &&
		ref.Version != versionpkg.Unknown {
		return ref.Version.String(), nil
	}
	return p.fetchVersion(ctx, gameVersion, ref.Version)
}

// fetchVersion selects the newest NeoForge version for the Minecraft
// version. stable excludes prerelease builds; beta accepts any build; any
// prefers a release and falls back to a prerelease.
func (p *Provider) fetchVersion(
	ctx context.Context,
	gameVersion versionpkg.Version,
	selector versionpkg.Version,
) (string, error) {
	body, err := p.network.CachedGetBytes(
		ctx,
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

	neoPrefix, ok := neoForgePrefix(gameVersion)
	if !ok {
		return "", fmt.Errorf(
			"cannot derive NeoForge version prefix from Minecraft version %s",
			gameVersion,
		)
	}

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
