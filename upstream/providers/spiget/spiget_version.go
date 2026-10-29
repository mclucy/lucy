package spiget

import (
	"context"
	"strings"

	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
)

func (p *Provider) resolveResourceByProjectName(
	ctx context.Context,
	name catalog.Name,
) (
	*resourceResponse,
	error,
) {
	if id, ok := parseNumericResourceID(name); ok {
		return p.getResource(ctx, id)
	}

	results, err := p.searchResources(ctx, name.String())
	if err != nil {
		return nil, err
	}

	for _, candidate := range results {
		if normalizedProjectName(candidate.Name) == name || strings.EqualFold(
			candidate.Name,
			name.String(),
		) {
			return p.getResource(ctx, candidate.ID)
		}
	}

	if len(results) == 1 {
		return p.getResource(ctx, results[0].ID)
	}

	return nil, ErrNoProject
}

func (p *Provider) resolveVersion(
	ctx context.Context,
	resource *resourceResponse,
	requested versionpkg.Version,
) (resolvedVersion, error) {
	switch requested {
	case "", versionpkg.Any, versionpkg.Beta, versionpkg.Stable:
		latest, err := p.getLatestVersion(ctx, resource.ID)
		if err != nil {
			return resolvedVersion{}, err
		}
		return NewResolvedVersion(*resource, *latest), nil
	}

	versions, err := p.listVersions(ctx, resource.ID)
	if err != nil {
		return resolvedVersion{}, err
	}
	for _, version := range versions {
		resolved := NewResolvedVersion(*resource, version)
		if resolved.Matches(requested) {
			return resolved, nil
		}
	}

	return resolvedVersion{}, ErrNoVersion
}
