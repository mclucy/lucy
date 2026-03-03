// Package routing contains source-to-provider bindings and source resolution
// policies.
//
// Responsibilities:
//   - Resolve SourceAuto against Platform into ordered provider candidates.
//   - Map explicit Source to exactly one provider when supported.
//   - Apply operation-aware routing policy (search/info/fetch/dependencies).
//   - Return typed selection errors for invalid/unsupported inputs.
//
// Non-responsibilities:
//   - Do not call provider APIs.
//   - Do not aggregate or merge upstream result payloads.
package routing

import (
	"errors"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/curseforge"
	"github.com/mclucy/lucy/upstream/githubsource"
	"github.com/mclucy/lucy/upstream/mcdr"
	"github.com/mclucy/lucy/upstream/modrinth"
)

var (
	ErrUnknownSource     = errors.New("unknown source")
	ErrUnsupportedSource = errors.New("unsupported source")
	ErrInvalidPlatform   = errors.New("invalid platform")
)

var autoProviders = []upstream.Provider{
	modrinth.Provider,
	mcdr.Provider,
}

var providerBySource = map[types.Source]upstream.Provider{
	types.SourceCurseForge: curseforge.Provider,
	types.SourceModrinth:   modrinth.Provider,
	types.SourceGitHub:     githubsource.Provider,
	types.SourceMCDR:       mcdr.Provider,
}

// ListAutoProviders returns the default ordered provider list used when
// source=auto and platform=all.
func ListAutoProviders() []upstream.Provider {
	res := make([]upstream.Provider, len(autoProviders))
	copy(res, autoProviders)
	return res
}

func GetProvider(src types.Source) (upstream.Provider, bool) {
	p, ok := providerBySource[src]
	return p, ok
}

// ResolveProviders resolves ordered provider candidates for a given operation,
// platform, and user-specified source.
func ResolveProviders(
	platform types.Platform,
	src types.Source,
) ([]upstream.Provider, error) {
	if src == types.SourceUnknown {
		return nil, ErrUnknownSource
	}

	if src != types.SourceAuto {
		provider, ok := GetProvider(src)
		if !ok {
			return nil, ErrUnsupportedSource
		}
		return []upstream.Provider{provider}, nil
	}

	switch platform {
	case types.AnyPlatform:
		return ListAutoProviders(), nil
	case types.Forge, types.Fabric, types.Neoforge:
		return []upstream.Provider{modrinth.Provider}, nil
	case types.Mcdr:
		return []upstream.Provider{mcdr.Provider}, nil
	default:
		return nil, ErrInvalidPlatform
	}
}
