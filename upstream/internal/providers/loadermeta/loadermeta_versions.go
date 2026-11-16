package loadermeta

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/version"
)

const metaTTL = 3 * 24 * time.Hour

// ResolveLoaderVersion resolves a loader selector into the concrete loader
// version the API publishes. Stable picks the newest release, Beta and Any
// pick the newest entry, and any other value must match exactly.
func (p *Provider) ResolveLoaderVersion(
	ctx context.Context,
	selector version.Version,
) (LoaderVersion, error) {
	versions, err := p.loaderVersions(ctx)
	if err != nil {
		return LoaderVersion{}, err
	}
	entry, err := selectLoaderVersion(
		versions,
		p.definition.StableFlag,
		selector,
	)
	if err != nil {
		return LoaderVersion{}, err
	}
	return LoaderVersion{
		Version: version.Version(entry.Version),
		Maven:   entry.Maven,
		Hashes:  cloneHashes(entry.Hashes),
	}, nil
}

// LatestGameVersion reports the newest Minecraft release the loader publishes
// builds for.
func (p *Provider) LatestGameVersion(
	ctx context.Context,
) (version.Version, error) {
	versions, err := p.gameVersions(ctx)
	if err != nil {
		return "", err
	}
	entry, err := selectGameVersion(versions, version.Stable)
	if err != nil {
		return "", err
	}
	return version.Version(entry.Version), nil
}

func (p *Provider) loaderVersions(
	ctx context.Context,
) ([]loaderVersionEntry, error) {
	var versions []loaderVersionEntry
	if err := p.fetchVersionsMeta(ctx, "loader", &versions); err != nil {
		return nil, err
	}
	return versions, nil
}

func (p *Provider) gameVersions(
	ctx context.Context,
) ([]gameVersionEntry, error) {
	var versions []gameVersionEntry
	if err := p.fetchVersionsMeta(ctx, "game", &versions); err != nil {
		return nil, err
	}
	return versions, nil
}

// fetchVersionsMeta reads one version list of the loader meta API.
func (p *Provider) fetchVersionsMeta(
	ctx context.Context,
	kind string,
	target any,
) error {
	endpoint := p.definition.APIBase + "/" +
		p.definition.APIVersion + "/versions/" + kind
	data, err := p.network.CachedGetBytes(
		ctx,
		endpoint,
		cache.BytesRequestOptions{
			Kind: cache.KindMetadata,
			TTL:  metaTTL,
		},
	)
	if err != nil {
		return fmt.Errorf(
			"fetch %s %s versions meta failed: %w",
			p.definition.ProjectID,
			kind,
			err,
		)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf(
			"parse %s %s versions meta failed: %w",
			p.definition.ProjectID,
			kind,
			err,
		)
	}
	return nil
}

// isStable reports whether a loader entry is a release. A loader that omits
// the stable flag marks a prerelease with a version suffix.
func isStable(entry loaderVersionEntry, stableFlag bool) bool {
	if stableFlag {
		return entry.Stable
	}
	return !strings.Contains(entry.Version, "-")
}

// selectLoaderVersion picks from the newest-first loader list. Stable picks
// the newest release, Beta and Any pick the newest entry, and any other value
// must match exactly.
func selectLoaderVersion(
	versions []loaderVersionEntry,
	stableFlag bool,
	selector version.Version,
) (loaderVersionEntry, error) {
	if selector.IsInvalid() || selector == "" {
		return loaderVersionEntry{}, errors.New("unknown loader selector")
	}
	if len(versions) == 0 {
		return loaderVersionEntry{}, errors.New(
			"no loader versions available",
		)
	}
	switch selector {
	case version.Stable:
		for _, entry := range versions {
			if isStable(entry, stableFlag) {
				return entry, nil
			}
		}
		return loaderVersionEntry{}, errors.New(
			"no stable loader version available",
		)
	case version.Beta, version.Any:
		return versions[0], nil
	}
	for _, entry := range versions {
		if entry.Version == selector.String() {
			return entry, nil
		}
	}
	return loaderVersionEntry{}, fmt.Errorf(
		"loader version %s not found", selector.String(),
	)
}

// selectGameVersion picks from the newest-first Minecraft version list.
func selectGameVersion(
	versions []gameVersionEntry,
	selector version.Version,
) (gameVersionEntry, error) {
	if selector.IsInvalid() || selector == "" {
		return gameVersionEntry{}, errors.New("unknown game selector")
	}
	if len(versions) == 0 {
		return gameVersionEntry{}, errors.New(
			"no game versions available",
		)
	}
	switch selector {
	case version.Stable:
		for _, entry := range versions {
			if entry.Stable {
				return entry, nil
			}
		}
		return gameVersionEntry{}, errors.New(
			"no stable Minecraft version in loader metadata",
		)
	case version.Beta, version.Any:
		return versions[0], nil
	}
	for _, entry := range versions {
		if entry.Version == selector.String() {
			return entry, nil
		}
	}
	return gameVersionEntry{}, fmt.Errorf(
		"game version %s not found", selector.String(),
	)
}
