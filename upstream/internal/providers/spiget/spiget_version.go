package spiget

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/catalog"
	versionpkg "github.com/mclucy/lucy/version"
)

// resolveResourceByProjectName resolves a resource for identity lookups such as
// info and build listing. It keeps the lenient single-search-hit fallback that
// explicit installs do not use.
func (p *Provider) resolveResourceByProjectName(
	ctx context.Context,
	name catalog.Name,
) (
	*resourceResponse,
	error,
) {
	resource, err := p.resolveCatalogueResource(ctx, name)
	if err == nil {
		return resource, nil
	}
	if !errors.Is(err, ErrNoProject) {
		return nil, err
	}

	// A lone search hit is accepted even when its name differs slightly; one
	// result cannot be confused with another project.
	results, err := p.searchResources(ctx, name.String())
	if err != nil {
		return nil, err
	}
	if len(results) == 1 && results[0].ID > 0 {
		return p.getResource(ctx, results[0].ID)
	}

	return nil, ErrNoProject
}

// resolveCatalogueResource resolves the explicit identity a package request
// carries. A numeric resource ID is Spiget's only stable, installable identity.
// A display name is accepted only when exactly one resource matches it:
// SpigotMC publishes distinct resources under identical names, and choosing one
// of them would install an arbitrary project.
func (p *Provider) resolveCatalogueResource(
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

	wanted := normalizedProjectName(name.String())
	matches := make([]resourceResponse, 0, 1)
	seen := make(map[int64]struct{}, len(results))
	for _, candidate := range results {
		if candidate.ID <= 0 {
			continue
		}
		if _, duplicate := seen[candidate.ID]; duplicate {
			continue
		}
		if normalizedProjectName(candidate.Name) != wanted &&
			!strings.EqualFold(candidate.Name, name.String()) {
			continue
		}
		seen[candidate.ID] = struct{}{}
		matches = append(matches, candidate)
	}

	switch len(matches) {
	case 0:
		return nil, ErrNoProject
	case 1:
		return p.getResource(ctx, matches[0].ID)
	default:
		ids := make([]string, 0, len(matches))
		for _, match := range matches {
			ids = append(ids, strconv.FormatInt(match.ID, 10))
		}
		return nil, fmt.Errorf(
			"%w: %q matches resources %s; use one Spiget resource ID",
			ErrAmbiguousProject,
			name,
			strings.Join(ids, ", "),
		)
	}
}

func (p *Provider) resolveVersion(
	ctx context.Context,
	resource *resourceResponse,
	requested versionpkg.Version,
) (resolvedVersion, error) {
	if err := downloadCapabilityError(*resource); err != nil {
		return resolvedVersion{}, err
	}

	currentVersionID := resource.currentVersionID()
	versions, err := p.listVersions(ctx, resource.ID)
	if err != nil {
		return resolvedVersion{}, err
	}

	// Only the current version has a download Lucy can fetch: the resource-scoped
	// endpoint always serves it, and the version-scoped endpoint is challenged by
	// Cloudflare. Resolving any other version would promise bytes that do not
	// arrive.
	for _, version := range versions {
		if version.ID != currentVersionID {
			continue
		}
		resolved := NewResolvedVersion(*resource, version)
		switch requested {
		case "", versionpkg.Any, versionpkg.Beta, versionpkg.Stable:
			return resolved, nil
		}
		if resolved.Matches(requested) {
			return resolved, nil
		}
		return resolvedVersion{}, fmt.Errorf(
			"%w: Spiget only serves the current version %s for download",
			ErrNoVersion,
			resolved.LucyVersion(),
		)
	}

	return resolvedVersion{}, ErrNoVersion
}

// downloadCapabilityError reports why a resource cannot provide a pinned
// artifact at all. Premium resources have no public file; every resource must
// report which version its single download URL serves; and an external URL must
// name a plugin archive, because SpigotMC external resources sometimes point at
// release pages instead.
func downloadCapabilityError(resource resourceResponse) error {
	if resource.Premium {
		return fmt.Errorf(
			"spiget: resource %d (%s) is a premium resource with no public download",
			resource.ID,
			resource.Name,
		)
	}
	if resource.currentVersionID() == 0 {
		return fmt.Errorf(
			"spiget: resource %d (%s) does not report which version its download serves",
			resource.ID,
			resource.Name,
		)
	}
	if resource.External {
		if archiveFilename(resource.File.ExternalURL) == "" {
			return fmt.Errorf(
				"spiget: resource %d (%s) downloads from %q, which is not a plugin archive pinned to a version",
				resource.ID,
				resource.Name,
				resource.File.ExternalURL,
			)
		}
		return nil
	}
	if normalizedFileExtension(resource.File.Type) == "" {
		return fmt.Errorf(
			"spiget: resource %d (%s) does not report the archive type of its download",
			resource.ID,
			resource.Name,
		)
	}
	return nil
}
