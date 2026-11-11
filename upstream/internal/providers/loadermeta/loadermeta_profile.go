package loadermeta

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/url"

	"github.com/mclucy/lucy/cache"
	"github.com/mclucy/lucy/version"
)

// ServerProfile fetches the launch profile of one game/loader pair. The API
// publishes no profile for an incompatible pair; that response is an error.
func (p *Provider) ServerProfile(
	ctx context.Context,
	game, loader version.Version,
) (Profile, error) {
	if floating(game) {
		return Profile{}, fmt.Errorf(
			"%s server profile requires a concrete Minecraft version",
			p.definition.ProjectID,
		)
	}
	if floating(loader) {
		return Profile{}, fmt.Errorf(
			"%s server profile requires a concrete loader version",
			p.definition.ProjectID,
		)
	}
	endpoint := p.definition.APIBase + "/" +
		p.definition.APIVersion + "/versions/loader/" +
		url.PathEscape(game.String()) + "/" +
		url.PathEscape(loader.String()) + "/server/json"
	data, err := p.network.CachedGetBytes(
		ctx,
		endpoint,
		cache.BytesRequestOptions{
			Kind: cache.KindMetadata,
			TTL:  metaTTL,
		},
	)
	if err != nil {
		return Profile{}, fmt.Errorf(
			"fetch %s server profile failed: %w",
			p.definition.ProjectID,
			err,
		)
	}
	var profile Profile
	if err := json.Unmarshal(data, &profile); err != nil {
		return Profile{}, fmt.Errorf(
			"parse %s server profile failed: %w",
			p.definition.ProjectID,
			err,
		)
	}
	return profile, nil
}

// floating reports whether a version names a selector instead of an exact
// version.
func floating(v version.Version) bool {
	switch v {
	case "", version.Any, version.Beta, version.Stable, version.Unknown,
		"latest":
		return true
	default:
		return false
	}
}
