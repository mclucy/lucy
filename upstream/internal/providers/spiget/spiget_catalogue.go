package spiget

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
)

// ErrAmbiguousProject reports a display-name lookup that matches more than one
// Spiget resource. SpigotMC publishes distinct resources under identical names,
// so the catalogue refuses to pick one of them.
var ErrAmbiguousProject = errors.New("spiget: project name is ambiguous")

// Candidates lists the downloadable version of one Spiget resource.
//
// Spiget exposes exactly one download per resource that Lucy can fetch:
//
//   - The resource-scoped endpoint (/resources/{id}/download) redirects to the
//     CDN copy of the CURRENT file. It accepts no version selector, so it can
//     only ever serve resource.Version.ID.
//   - The version-scoped endpoint (/versions/{id}/download) redirects to
//     spigotmc.org, which answers automated clients with a Cloudflare challenge,
//     so it yields no bytes and no usable pinned URL.
//
// Candidates therefore reports the current version only, identified by the
// numeric resource and version IDs. Listing older versions would resolve
// metadata and then fail during materialization. The candidate URL is mutable
// upstream, so the lock records the SHA-256 of the bytes actually fetched and a
// later re-download that differs fails verification instead of installing
// different bytes under the pinned version.
//
// Spiget reports no hashes, release channel, dependency declarations, or
// per-version game and loader compatibility. Candidates therefore assert none of
// them: Channel stays unknown and both DependenciesUnknown and
// CompatibilityUnknown are set. Resource-level testedVersions describe the
// resource as a whole, not one published version, so they are not used as
// compatibility evidence; the requested Minecraft version cannot be verified or
// filtered here.
func (p *Provider) Candidates(
	ctx context.Context,
	project, _ string,
	loader catalog.Ecosystem,
) ([]upstream.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !spigetServesLoader(loader) {
		return nil, fmt.Errorf(
			"spiget: %s packages are not published upstream; Spiget serves Bukkit and Paper plugins",
			loader,
		)
	}

	resource, err := p.resolveCatalogueResource(ctx, catalog.Name(project))
	if err != nil {
		return nil, err
	}
	if err := downloadCapabilityError(*resource); err != nil {
		return nil, err
	}

	currentVersionID := resource.currentVersionID()
	versions, err := p.listVersions(ctx, resource.ID)
	if err != nil {
		return nil, err
	}

	downloadURL := resourceDownloadURL(resource.ID)
	if resource.External {
		downloadURL = strings.TrimSpace(resource.File.ExternalURL)
	}

	for _, version := range versions {
		if version.ID != currentVersionID {
			continue
		}
		return []upstream.Candidate{{
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderSpiget,
				ProjectID: strconv.FormatInt(resource.ID, 10),
				ReleaseID: strconv.FormatInt(version.ID, 10),
				FileID:    strconv.FormatInt(version.ID, 10),
				Version:   spigetVersion(version.Name, version.ID),
				Filename: artifactFilename(
					resource.ID,
					resource.Name,
					version.Name,
					version.ID,
					resource.File.Type,
					downloadURL,
				),
				URL: downloadURL,
			},
			Channel:              upstream.ReleaseChannelUnknown,
			PublishedAt:          releaseTime(version.ReleaseDate),
			PrimaryFile:          true,
			DependenciesUnknown:  true,
			CompatibilityUnknown: true,
		}}, nil
	}

	return nil, fmt.Errorf(
		"spiget: resource %d (%s) does not publish its current version %d in its version history",
		resource.ID,
		resource.Name,
		currentVersionID,
	)
}

// spigetServesLoader reports whether Lucy may hand this ecosystem to Spiget.
// SpigotMC publishes Bukkit plugins, which Paper also loads; an unspecified
// ecosystem states no constraint to check.
func spigetServesLoader(loader catalog.Ecosystem) bool {
	switch loader {
	case catalog.EcosystemUnspecified, catalog.EcosystemBukkit,
		catalog.EcosystemPaper:
		return true
	default:
		return false
	}
}

func releaseTime(unix int64) time.Time {
	if unix <= 0 {
		return time.Time{}
	}
	return time.Unix(unix, 0).UTC()
}
