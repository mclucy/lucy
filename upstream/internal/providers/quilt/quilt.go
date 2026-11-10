package quilt

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/maven"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/version"
)

const (
	metaBaseURL        = "https://meta.quiltmc.org"
	loaderMavenBaseURL = "https://maven.quiltmc.org/repository/release/"
	loaderCoordinate   = "org.quiltmc:quilt-loader"
)

// Provider resolves Quilt loader metadata, server profiles, and Maven
// artifacts over the shared network cache.
type Provider struct {
	network *cache.Client
}

// New constructs a Quilt provider. network supplies the cached HTTP transport;
// construction performs no requests.
func New(network *cache.Client) *Provider {
	return &Provider{network: network}
}

func (p *Provider) Provider() catalog.Provider {
	return catalog.ProviderQuilt
}

// LoaderVersion is one entry of Quilt Meta v3's loader version list. The list
// carries no stable flag: a prerelease is encoded in the version suffix.
type LoaderVersion struct {
	Maven     string            `json:"maven"`
	Version   string            `json:"version"`
	Build     int               `json:"build"`
	Separator string            `json:"separator"`
	FileSize  int64             `json:"file_size"`
	Hashes    map[string]string `json:"hashes"`
}

// GameVersion is one entry of Quilt Meta v3's game version list. Unlike loader
// entries, game entries do carry a stable flag.
type GameVersion struct {
	Version string `json:"version"`
	Stable  bool   `json:"stable"`
}

// Library is one Maven library declared by a server profile. URL is the
// repository base, not the artifact URL.
type Library struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ServerProfile is the launch profile Quilt Meta publishes for one
// game/loader pair.
type ServerProfile struct {
	ID                string    `json:"id"`
	InheritsFrom      string    `json:"inheritsFrom"`
	MainClass         string    `json:"mainClass"`
	LauncherMainClass string    `json:"launcherMainClass"`
	Libraries         []Library `json:"libraries"`
}

// ResolveVersionSelector resolves a loader selector into the concrete loader
// version Quilt Meta publishes. Stable picks the newest release, Beta and Any
// pick the newest entry, and any other value must match exactly.
func (p *Provider) ResolveVersionSelector(
	ctx context.Context,
	id catalog.VersionedRef,
) (catalog.VersionedRef, error) {
	versions, err := p.LoaderVersions(ctx)
	if err != nil {
		return id, err
	}
	entry, err := selectLoaderVersion(versions, id.Version)
	if err != nil {
		return id, err
	}
	return catalog.VersionedRef{
		Ref:       catalog.Ref{Name: "quilt"},
		Ecosystem: catalog.EcosystemQuilt,
		Version:   version.Version(entry.Version),
	}, nil
}

// ResolveGameVersionSelector resolves a game selector into the concrete
// Minecraft version Quilt Meta publishes. Stable picks the newest release,
// Beta and Any pick the newest entry, and any other value must match exactly.
func (p *Provider) ResolveGameVersionSelector(
	ctx context.Context,
	selector version.Version,
) (version.Version, error) {
	versions, err := p.GameVersions(ctx)
	if err != nil {
		return "", err
	}
	entry, err := selectGameVersion(versions, selector)
	if err != nil {
		return "", err
	}
	return version.Version(entry.Version), nil
}

// LatestGameVersion reports the newest Minecraft release Quilt Meta lists.
func (p *Provider) LatestGameVersion(
	ctx context.Context,
) (version.Version, error) {
	return p.ResolveGameVersionSelector(ctx, version.Stable)
}

// LoaderVersions lists every loader version Quilt Meta publishes, newest
// first.
func (p *Provider) LoaderVersions(
	ctx context.Context,
) ([]LoaderVersion, error) {
	var versions []LoaderVersion
	if err := p.fetchVersionsMeta(ctx, "loader", &versions); err != nil {
		return nil, err
	}
	return versions, nil
}

// GameVersions lists every Minecraft version Quilt Meta publishes, newest
// first.
func (p *Provider) GameVersions(ctx context.Context) ([]GameVersion, error) {
	var versions []GameVersion
	if err := p.fetchVersionsMeta(ctx, "game", &versions); err != nil {
		return nil, err
	}
	return versions, nil
}

// ServerProfile fetches the launch profile of one game/loader pair. Quilt
// publishes no profile for an incompatible pair; that response is an error.
func (p *Provider) ServerProfile(
	ctx context.Context,
	game, loader version.Version,
) (ServerProfile, error) {
	if floating(game) {
		return ServerProfile{}, errors.New(
			"quilt server profile requires a concrete Minecraft version",
		)
	}
	if floating(loader) {
		return ServerProfile{}, errors.New(
			"quilt server profile requires a concrete loader version",
		)
	}
	endpoint := metaBaseURL + "/v3/versions/loader/" +
		url.PathEscape(game.String()) + "/" +
		url.PathEscape(loader.String()) + "/server/json"
	data, err := p.network.CachedGetBytes(
		ctx,
		endpoint,
		cache.BytesRequestOptions{
			Kind: cache.KindMetadata,
			TTL:  3 * 24 * time.Hour,
		},
	)
	if err != nil {
		return ServerProfile{}, fmt.Errorf(
			"fetch quilt server profile failed: %w",
			err,
		)
	}
	var profile ServerProfile
	if err := json.Unmarshal(data, &profile); err != nil {
		return ServerProfile{}, fmt.Errorf(
			"parse quilt server profile failed: %w",
			err,
		)
	}
	return profile, nil
}

// LoaderArtifact returns the pinned Maven artifact of a loader version. The
// entry's published checksums are adopted as the artifact's hashes.
func (p *Provider) LoaderArtifact(
	ctx context.Context,
	loader version.Version,
) (catalog.Artifact, error) {
	if floating(loader) {
		return catalog.Artifact{}, errors.New(
			"quilt loader artifact requires a concrete loader version",
		)
	}
	versions, err := p.LoaderVersions(ctx)
	if err != nil {
		return catalog.Artifact{}, err
	}
	for _, entry := range versions {
		if entry.Version == loader.String() {
			return loaderArtifact(entry)
		}
	}
	return catalog.Artifact{}, fmt.Errorf(
		"quilt loader version %s not found",
		loader,
	)
}

// PinnedInputs turns a server profile into bootstrap inputs. Every profile
// library except the loader itself is pinned to its Maven URL and the
// repository's published checksum. The loader is the core artifact, so it is
// not repeated as an input.
func (p *Provider) PinnedInputs(
	ctx context.Context,
	profile ServerProfile,
	loader version.Version,
) ([]server.BootstrapInput, error) {
	hashes := make(map[string]map[string]string, len(profile.Libraries))
	for _, lib := range profile.Libraries {
		if isLoaderLibrary(lib.Name) {
			continue
		}
		sum, err := p.publishedSHA256(ctx, lib)
		if err != nil {
			return nil, err
		}
		hashes[lib.Name] = map[string]string{"sha256": sum}
	}
	return profileInputs(profile, loader, hashes)
}

func (p *Provider) publishedSHA256(
	ctx context.Context,
	lib Library,
) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	artifact, err := libraryArtifact(lib)
	if err != nil {
		return "", err
	}
	data, err := p.network.CachedGetBytes(
		ctx,
		artifact.URL+".sha256",
		cache.BytesRequestOptions{Kind: cache.KindMetadata},
	)
	if err != nil {
		return "", fmt.Errorf(
			"no trusted checksum for %s: %w",
			artifact.Filename,
			err,
		)
	}
	sum := strings.TrimSpace(string(data))
	if len(sum) != 64 {
		return "", fmt.Errorf("malformed checksum for %s", artifact.Filename)
	}
	return sum, nil
}

func (p *Provider) fetchVersionsMeta(
	ctx context.Context,
	endpoint string,
	target any,
) (err error) {
	data, err := p.network.CachedGetBytes(
		ctx,
		metaBaseURL+"/v3/versions/"+endpoint,
		cache.BytesRequestOptions{
			Kind: cache.KindMetadata,
			TTL:  3 * 24 * time.Hour,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"fetch quilt %s versions meta failed: %w",
			endpoint,
			err,
		)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf(
			"parse quilt %s versions meta failed: %w",
			endpoint,
			err,
		)
	}
	return nil
}

// selectLoaderVersion picks from Quilt Meta's newest-first loader list. The
// list has no stable flag, so a prerelease is the version suffix itself.
func selectLoaderVersion(
	versions []LoaderVersion,
	selector version.Version,
) (LoaderVersion, error) {
	if selector == version.Unknown || selector == "" {
		return LoaderVersion{}, errors.New("unknown quilt loader selector")
	}
	if len(versions) == 0 {
		return LoaderVersion{}, errors.New("no quilt loader versions available")
	}
	switch selector {
	case version.Stable:
		for _, entry := range versions {
			if !strings.Contains(entry.Version, "-") {
				return entry, nil
			}
		}
		return LoaderVersion{}, errors.New(
			"no stable quilt loader version available",
		)
	case version.Beta, version.Any:
		return versions[0], nil
	}
	for _, entry := range versions {
		if entry.Version == selector.String() {
			return entry, nil
		}
	}
	return LoaderVersion{}, fmt.Errorf(
		"quilt loader version %s not found",
		selector.String(),
	)
}

// selectGameVersion picks from Quilt Meta's newest-first game list.
func selectGameVersion(
	versions []GameVersion,
	selector version.Version,
) (GameVersion, error) {
	if selector == version.Unknown || selector == "" {
		return GameVersion{}, errors.New("unknown quilt game selector")
	}
	if len(versions) == 0 {
		return GameVersion{}, errors.New("no quilt game versions available")
	}
	switch selector {
	case version.Stable:
		for _, entry := range versions {
			if entry.Stable {
				return entry, nil
			}
		}
		return GameVersion{}, errors.New(
			"no stable Minecraft version in quilt metadata",
		)
	case version.Beta, version.Any:
		return versions[0], nil
	}
	for _, entry := range versions {
		if entry.Version == selector.String() {
			return entry, nil
		}
	}
	return GameVersion{}, fmt.Errorf(
		"quilt game version %s not found",
		selector.String(),
	)
}

// loaderArtifact pins a loader entry to its Maven URL and published hashes.
func loaderArtifact(entry LoaderVersion) (catalog.Artifact, error) {
	coordinate := entry.Maven
	if coordinate == "" {
		coordinate = loaderCoordinate + ":" + entry.Version
	}
	p, err := maven.Path(coordinate)
	if err != nil {
		return catalog.Artifact{}, fmt.Errorf(
			"invalid quilt loader Maven coordinate %q: %w",
			coordinate,
			err,
		)
	}
	return catalog.Artifact{
		Provider:  catalog.ProviderQuilt,
		ProjectID: "quilt-loader",
		Version:   version.Version(entry.Version),
		Filename:  path.Base(p),
		URL:       loaderMavenBaseURL + p,
		Hashes:    cloneHashes(entry.Hashes),
	}, nil
}

// libraryPath returns the repository-relative Maven path of a profile library.
func libraryPath(lib Library) (string, error) {
	if strings.TrimSpace(lib.Name) == "" {
		return "", errors.New("quilt profile library has no Maven coordinate")
	}
	return maven.Path(lib.Name)
}

// libraryArtifact describes a profile library at its Maven URL. Checksums are
// pinned separately because a profile declares none.
func libraryArtifact(lib Library) (catalog.Artifact, error) {
	p, err := libraryPath(lib)
	if err != nil {
		return catalog.Artifact{}, err
	}
	base := strings.TrimRight(strings.TrimSpace(lib.URL), "/")
	if base == "" {
		return catalog.Artifact{}, fmt.Errorf(
			"quilt profile library %s has no Maven repository URL",
			lib.Name,
		)
	}
	return catalog.Artifact{
		Provider:  catalog.ProviderMaven,
		ProjectID: lib.Name,
		Filename:  path.Base(p),
		URL:       base + "/" + p,
		Hashes:    map[string]string{},
	}, nil
}

// profileInputs translates a profile into library bootstrap inputs. Profile
// order is preserved and the loader library is skipped: bootstrap places the
// loader core artifact at the same path.
func profileInputs(
	profile ServerProfile,
	loader version.Version,
	hashes map[string]map[string]string,
) ([]server.BootstrapInput, error) {
	inputs := make([]server.BootstrapInput, 0, len(profile.Libraries))
	for _, lib := range profile.Libraries {
		if isLoaderLibrary(lib.Name) {
			continue
		}
		p, err := libraryPath(lib)
		if err != nil {
			return nil, err
		}
		artifact, err := libraryArtifact(lib)
		if err != nil {
			return nil, err
		}
		pinned := hashes[lib.Name]
		if len(pinned) == 0 {
			return nil, fmt.Errorf(
				"quilt profile library %s has no pinned checksum",
				lib.Name,
			)
		}
		artifact.Version = loader
		artifact.Hashes = cloneHashes(pinned)
		inputs = append(inputs, server.BootstrapInput{
			Path:     "libraries/" + p,
			Artifact: artifact,
		})
	}
	return inputs, nil
}

// isLoaderLibrary reports whether a profile coordinate is the Quilt loader
// itself, which bootstrap installs from the core artifact.
func isLoaderLibrary(coordinate string) bool {
	base, _, _ := strings.Cut(coordinate, "@")
	parts := strings.Split(base, ":")
	return len(parts) >= 2 &&
		parts[0] == "org.quiltmc" && parts[1] == "quilt-loader"
}

func cloneHashes(hashes map[string]string) map[string]string {
	clone := make(map[string]string, len(hashes))
	for name, value := range hashes {
		clone[name] = value
	}
	return clone
}

// floating reports whether a version names a selector instead of an exact
// version.
func floating(v version.Version) bool {
	switch v {
	case "", version.Any, version.Beta, version.Stable, version.Unknown, "latest":
		return true
	default:
		return false
	}
}
