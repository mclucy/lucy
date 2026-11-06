package hangar

import (
	"context"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
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

type hangarDependencies struct {
	version  *hangarVersion
	platform catalog.Ecosystem
}

func (h *hangarDependencies) ToDependencySet() upstream.DependencySet {
	result := upstream.DependencySet{FromArtifact: true}
	keys, supported := hangarPlatformKeysFor(h.platform)
	if !supported {
		return result
	}
	if len(keys) == 0 {
		keys = hangarPreferredPlatforms
	}

	seen := make(map[int64]struct{})
	for _, key := range keys {
		declared, known := h.version.platformPluginDependencies(key)
		if !known {
			continue
		}
		for _, dep := range declared {
			if dep.Name == "" || dep.ExternalURL != nil && *dep.ExternalURL != "" {
				continue
			}
			if dep.ProjectID == nil || *dep.ProjectID <= 0 {
				continue
			}
			if _, duplicate := seen[*dep.ProjectID]; duplicate {
				continue
			}
			seen[*dep.ProjectID] = struct{}{}
			result.Dependencies = append(
				result.Dependencies, catalog.Dependency{
					ID: catalog.VersionedRef{
						Ref: catalog.Ref{
							Name:     input.ToProjectName(dep.Name),
							Provider: catalog.ProviderUnspecified,
						},
						Ecosystem: catalog.EcosystemUnspecified,
					},
					Mandatory: dep.Required,
				},
			)
		}
	}
	return result
}

func (p *Provider) resolveVersion(
	ctx context.Context,
	id catalog.VersionedRef,
) (*hangarVersion, error) {
	versions, err := p.listVersions(ctx, id.Name)
	if err != nil {
		return nil, err
	}

	switch id.Version {
	case versionpkg.Any, versionpkg.Beta:
		return selectLatestVersion(versions, id.Ecosystem, id.Version)
	case versionpkg.Stable:
		return selectLatestCompatibleVersion(versions, id.Ecosystem)
	default:
		for i := range versions {
			if versions[i].Name != id.Version.String() {
				continue
			}
			if !id.Ecosystem.IsUnspecified() &&
				!versions[i].hasDownloadForPlatform(id.Ecosystem) {
				return nil, ErrNoVersion
			}
			return &versions[i], nil
		}
		return nil, ErrNoVersion
	}
}

// selectLatestVersion selects the newest version for any/beta selectors.
// any prefers a release channel and falls back to any channel; beta accepts
// any channel. A platform-specific request never falls back to another
// platform's artifact.
func selectLatestVersion(
	versions []hangarVersion,
	platform catalog.Ecosystem,
	selector versionpkg.Version,
) (*hangarVersion, error) {
	keys, supported := hangarPlatformKeysFor(platform)
	if !supported {
		return nil, ErrNoVersion
	}
	if len(keys) == 0 {
		keys = hangarPreferredPlatforms
	}
	for _, key := range keys {
		if selector == versionpkg.Any {
			if version := firstVersionMatching(versions, key, true); version != nil {
				return version, nil
			}
		}
		if version := firstVersionMatching(versions, key, false); version != nil {
			return version, nil
		}
	}
	return nil, ErrNoVersion
}

// selectLatestCompatibleVersion selects the newest release-channel version
// that publishes an artifact for the platform. stable never falls back to a
// prerelease channel.
func selectLatestCompatibleVersion(
	versions []hangarVersion,
	platform catalog.Ecosystem,
) (*hangarVersion, error) {
	keys, supported := hangarPlatformKeysFor(platform)
	if !supported {
		return nil, ErrNoVersion
	}
	if len(keys) == 0 {
		keys = hangarPreferredPlatforms
	}
	for _, key := range keys {
		if version := firstVersionMatching(versions, key, true); version != nil {
			return version, nil
		}
	}
	return nil, ErrNoVersion
}

func firstVersionMatching(
	versions []hangarVersion,
	platformKey string,
	requireRelease bool,
) *hangarVersion {
	for i := range versions {
		version := &versions[i]
		if _, ok := version.downloadForPlatformKey(platformKey); !ok {
			continue
		}
		if requireRelease && !version.IsReleaseChannel() {
			continue
		}
		return version
	}
	return nil
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

func (v *hangarVersion) hasDownloadForPlatform(
	platform catalog.Ecosystem,
) bool {
	_, ok := v.downloadForPlatform(platform)
	return ok
}

func (v *hangarVersion) downloadForPlatform(
	platform catalog.Ecosystem,
) (hangarDownload, bool) {
	keys, supported := hangarPlatformKeysFor(platform)
	if !supported {
		return hangarDownload{}, false
	}
	if len(keys) == 0 {
		keys = hangarPreferredPlatforms
	}
	for _, key := range keys {
		if download, ok := v.downloadForPlatformKey(key); ok {
			return download, true
		}
	}
	return hangarDownload{}, false
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
