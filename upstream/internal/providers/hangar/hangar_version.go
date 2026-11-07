package hangar

import (
	"strings"

	"github.com/mclucy/lucy/catalog"
)

// Hangar identifies plugin platforms by these names in both the downloads map
// and the platformDependencies map. Waterfall is the BungeeCord-compatible
// proxy platform Hangar hosts; Velocity is a separate proxy API. Hangar never
// hosts Fabric, Forge, NeoForge, or Vanilla artifacts.
const (
	hangarPlatformPaper     = "PAPER"
	hangarPlatformWaterfall = "WATERFALL"
	hangarPlatformVelocity  = "VELOCITY"
)

// hangarPreferredPlatforms orders platforms for a request that does not name
// one. The order is a documented default, never a fallback from an explicitly
// requested platform.
var hangarPreferredPlatforms = []string{
	hangarPlatformPaper,
	hangarPlatformWaterfall,
	hangarPlatformVelocity,
}

// hangarPlatformKeysFor maps a package ecosystem to Hangar platform
// identifiers in preference order. An empty slice with supported true means
// the request did not name a platform. supported false means Hangar cannot
// serve the ecosystem at all.
func hangarPlatformKeysFor(ecosystem catalog.Ecosystem) (
	[]string,
	bool,
) {
	switch catalog.Ecosystem(
		strings.ToLower(strings.TrimSpace(string(ecosystem))),
	) {
	case "":
		return nil, true
	case catalog.EcosystemPaper, catalog.EcosystemBukkit:
		return []string{hangarPlatformPaper}, true
	case catalog.EcosystemBungeeCord:
		return []string{hangarPlatformWaterfall}, true
	case catalog.EcosystemVelocity:
		return []string{hangarPlatformVelocity}, true
	default:
		return nil, false
	}
}

// hangarPlatformTracksMinecraft reports whether the platform's dependency
// list enumerates Minecraft versions. Velocity lists Velocity API versions
// instead, so a Minecraft version cannot be verified against it.
func hangarPlatformTracksMinecraft(platformKey string) bool {
	switch strings.ToUpper(platformKey) {
	case hangarPlatformPaper, hangarPlatformWaterfall:
		return true
	default:
		return false
	}
}

// downloadForPlatformKey returns the platform's download only when it carries
// a usable URL. A declared platform without a fetchable artifact is not a
// downloadable build.
func (v *hangarVersion) downloadForPlatformKey(
	platformKey string,
) (hangarDownload, bool) {
	for key, download := range v.Downloads {
		if !strings.EqualFold(key, platformKey) {
			continue
		}
		if download.URL() == "" || download.metadataFilename() == "" {
			return hangarDownload{}, false
		}
		return download, true
	}
	return hangarDownload{}, false
}
