package routing

import (
	"context"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
)

const (
	paperProjectURL  = "https://fill.papermc.io/v3/projects/paper"
	purpurProjectURL = "https://api.purpurmc.org/v2/purpur"
)

// LatestMinecraftVersion reports the newest Minecraft version a server product
// serves. It answers a floating Minecraft request: each product names the
// version it builds for, which can lag Mojang's release channel.
func (r *Router) LatestMinecraftVersion(
	ctx context.Context,
	product server.Product,
) (version.Version, error) {
	switch product {
	case server.ProductMinecraft:
		return r.LatestMojangMinecraft(ctx, false)
	case server.ProductFabric:
		return r.fabric.LatestGameVersion(ctx)
	case server.ProductForge:
		return r.forge.LatestMinecraftVersion(ctx)
	case server.ProductNeoForge:
		return r.neoforge.LatestMinecraftVersion(ctx)
	case server.ProductPaper:
		return r.latestPaperMinecraft(ctx)
	case server.ProductPurpur:
		return r.latestPurpurMinecraft(ctx)
	default:
		return "", fmt.Errorf(
			"server product %q publishes no Minecraft versions",
			product,
		)
	}
}

// LatestMojangMinecraft reports the newest Minecraft version on Mojang's
// release channel, or on the snapshot channel when snapshot is set.
func (r *Router) LatestMojangMinecraft(
	ctx context.Context,
	snapshot bool,
) (version.Version, error) {
	manifest, err := r.mojang.VersionManifest(ctx)
	if err != nil {
		return "", err
	}
	if snapshot {
		return version.Version(manifest.Latest.Snapshot), nil
	}
	return version.Version(manifest.Latest.Release), nil
}

// MinecraftReleases reports Mojang's release-channel versions, newest first.
func (r *Router) MinecraftReleases(
	ctx context.Context,
) ([]version.Version, error) {
	manifest, err := r.mojang.VersionManifest(ctx)
	if err != nil {
		return nil, err
	}
	releases := make([]version.Version, 0, len(manifest.Versions))
	for _, entry := range manifest.Versions {
		if entry.Type != "release" {
			continue
		}
		releases = append(releases, version.Version(entry.ID))
	}
	return releases, nil
}

// latestPaperMinecraft reports the newest release Paper builds. The project
// endpoint groups versions by major line and lists prereleases beside them.
func (r *Router) latestPaperMinecraft(ctx context.Context) (version.Version, error) {
	var project struct {
		Versions map[string][]string `json:"versions"`
	}
	if err := r.Metadata(ctx, paperProjectURL, &project); err != nil {
		return "", err
	}
	versions := make([]version.Version, 0, len(project.Versions))
	for _, group := range project.Versions {
		for _, id := range group {
			if isMinecraftRelease(id) {
				versions = append(versions, version.Version(id))
			}
		}
	}
	return version.NewestMinecraft(versions)
}

func (r *Router) latestPurpurMinecraft(
	ctx context.Context,
) (version.Version, error) {
	var project struct {
		Versions []string `json:"versions"`
	}
	if err := r.Metadata(ctx, purpurProjectURL, &project); err != nil {
		return "", err
	}
	versions := make([]version.Version, 0, len(project.Versions))
	for _, id := range project.Versions {
		if isMinecraftRelease(id) {
			versions = append(versions, version.Version(id))
		}
	}
	return version.NewestMinecraft(versions)
}

// isMinecraftRelease rejects snapshots, prereleases, and release candidates,
// which carry a suffix the release grammar does not accept.
func isMinecraftRelease(id string) bool {
	return !strings.Contains(id, "-")
}
