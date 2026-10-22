package spiget

import (
	"strings"

	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
)

func resolveResourceByProjectName(name catalog.Name) (
	*resourceResponse,
	error,
) {
	if id, ok := parseNumericResourceID(name); ok {
		return getResource(id)
	}

	results, err := searchResources(name.String())
	if err != nil {
		return nil, err
	}

	for _, candidate := range results {
		if normalizedProjectName(candidate.Name) == name || strings.EqualFold(
			candidate.Name,
			name.String(),
		) {
			return getResource(candidate.ID)
		}
	}

	if len(results) == 1 {
		return getResource(results[0].ID)
	}

	return nil, ErrNoProject
}

func resolveVersion(
	resource *resourceResponse,
	requested versionpkg.Version,
) (resolvedVersion, error) {
	switch requested {
	case "", versionpkg.Any, versionpkg.Beta, versionpkg.Stable:
		latest, err := getLatestVersion(resource.ID)
		if err != nil {
			return resolvedVersion{}, err
		}
		return NewResolvedVersion(*resource, *latest), nil
	}

	versions, err := listVersions(resource.ID)
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
