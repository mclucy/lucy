package routing

import (
	"context"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream/providers/fabric"
	"github.com/mclucy/lucy/upstream/providers/forge"
	"github.com/mclucy/lucy/upstream/providers/mojang"
	"github.com/mclucy/lucy/upstream/providers/neoforge"
	"github.com/mclucy/lucy/version"
)

const (
	paperProjectURL  = "https://fill.papermc.io/v3/projects/paper"
	purpurProjectURL = "https://api.purpurmc.org/v2/purpur"
)

// LatestMinecraftVersion reports the newest Minecraft version a server product
// serves. It answers a floating Minecraft request: each product names the
// version it builds for, which can lag Mojang's release channel.
func LatestMinecraftVersion(
	ctx context.Context,
	product server.Product,
) (version.Version, error) {
	switch product {
	case server.ProductMinecraft:
		return LatestMojangMinecraft(false)
	case server.ProductFabric:
		return fabric.LatestGameVersion()
	case server.ProductForge:
		return forge.LatestMinecraftVersion()
	case server.ProductNeoForge:
		return neoforge.LatestMinecraftVersion()
	case server.ProductPaper:
		return latestPaperMinecraft(ctx)
	case server.ProductPurpur:
		return latestPurpurMinecraft(ctx)
	default:
		return "", fmt.Errorf(
			"server product %q publishes no Minecraft versions",
			product,
		)
	}
}

// LatestMojangMinecraft reports the newest Minecraft version on Mojang's
// release channel, or on the snapshot channel when snapshot is set.
func LatestMojangMinecraft(snapshot bool) (version.Version, error) {
	manifest, err := mojang.FetchVersionManifest()
	if err != nil {
		return "", err
	}
	if snapshot {
		return version.Version(manifest.Latest.Snapshot), nil
	}
	return version.Version(manifest.Latest.Release), nil
}

// latestPaperMinecraft reports the newest release Paper builds. The project
// endpoint groups versions by major line and lists prereleases beside them.
func latestPaperMinecraft(ctx context.Context) (version.Version, error) {
	var project struct {
		Versions map[string][]string `json:"versions"`
	}
	if err := Metadata(ctx, paperProjectURL, &project); err != nil {
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

func latestPurpurMinecraft(ctx context.Context) (version.Version, error) {
	var project struct {
		Versions []string `json:"versions"`
	}
	if err := Metadata(ctx, purpurProjectURL, &project); err != nil {
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
