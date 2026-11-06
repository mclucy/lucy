package hangar

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

// Candidates lists Hangar releases for one explicit project identity. Each
// candidate comes from the requested Hangar platform only: Hangar publishes
// platform-specific downloads, so another platform's file is never a
// substitute. Minecraft compatibility is enforced only where Hangar's
// platformDependencies enumerate Minecraft versions; otherwise the candidate
// carries CompatibilityUnknown instead of a compatibility claim.
func (p *Provider) Candidates(
	ctx context.Context,
	project, minecraft string,
	loader catalog.Ecosystem,
) ([]upstream.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	platformKeys, supported := hangarPlatformKeysFor(loader)
	if !supported {
		return nil, fmt.Errorf(
			"hangar serves Paper, Waterfall, and Velocity plugins; %s is not a Hangar platform",
			loader,
		)
	}

	resolved, err := p.getProject(ctx, catalog.Name(project))
	if err != nil {
		return nil, err
	}
	if resolved.ID <= 0 {
		return nil, fmt.Errorf(
			"%w: project %s has no numeric identity",
			ErrInvalidAPIResponse,
			project,
		)
	}

	versions, err := p.listProjectVersions(ctx, resolved)
	if err != nil {
		return nil, err
	}

	projectID := strconv.FormatInt(resolved.ID, 10)
	minecraftVersion := versionpkg.Version(
		strings.TrimSpace(minecraft),
	)
	minecraftKnown := minecraftVersion != "" &&
		!minecraftVersion.CanInfer() &&
		!minecraftVersion.IsInvalid()

	candidates := make([]upstream.Candidate, 0, len(versions))
	for i := range versions {
		version := &versions[i]
		if version.ProjectID != 0 && version.ProjectID != resolved.ID {
			continue
		}

		platformKey, download, ok := version.candidateDownload(platformKeys)
		if !ok {
			continue
		}

		filename := download.metadataFilename()
		fileURL := download.URL()
		if filename == "" || fileURL == "" ||
			!strings.HasSuffix(strings.ToLower(filename), ".jar") {
			// External links and non-jar uploads have no digest or filename
			// from Hangar metadata, so the lock could not pin them honestly.
			continue
		}

		compatibilityUnknown := false
		if hangarPlatformTracksMinecraft(platformKey) {
			declared, present := version.platformDependenciesFor(platformKey)
			switch {
			case !present:
				compatibilityUnknown = true
			case !minecraftKnown:
				compatibilityUnknown = true
			case !containsMinecraft(declared, string(minecraftVersion)):
				continue
			}
		} else {
			// Velocity platform dependencies list Velocity versions, not
			// Minecraft versions, so Hangar cannot verify this claim.
			compatibilityUnknown = true
		}

		dependencies, dependenciesKnown := version.candidateDependencies(
			platformKey,
		)
		hashes := map[string]string{}
		if hash := download.SHA256(); hash != "" {
			hashes["sha256"] = hash
		}

		candidates = append(candidates, upstream.Candidate{
			Artifact: catalog.Artifact{
				Provider:  catalog.ProviderHangar,
				ProjectID: projectID,
				ReleaseID: version.Name,
				FileID:    strconv.FormatInt(version.ID, 10),
				Version:   versionpkg.Version(version.Name),
				Filename:  filename,
				URL:       fileURL,
				Hashes:    hashes,
			},
			Channel:              upstream.ParseReleaseChannel(version.Channel.Name),
			PublishedAt:          hangarPublishedAt(version.CreatedAt),
			PrimaryFile:          true,
			Dependencies:         dependencies,
			DependenciesUnknown:  !dependenciesKnown,
			CompatibilityUnknown: compatibilityUnknown,
		})
	}

	if len(candidates) == 0 {
		return nil, fmt.Errorf(
			"hangar project %s has no downloadable %s build for Minecraft %s",
			resolved.ProjectRef().LookupPath(),
			loader,
			minecraft,
		)
	}
	return candidates, nil
}

// candidateDownload selects the download for exactly the requested platform.
// An unspecified ecosystem uses the documented preferred-platform order. It
// never crosses from an explicitly requested platform to another platform.
func (v *hangarVersion) candidateDownload(
	requested []string,
) (string, hangarDownload, bool) {
	platforms := requested
	if len(platforms) == 0 {
		platforms = hangarPreferredPlatforms
	}
	for _, platform := range platforms {
		download, ok := v.downloadForPlatformKey(platform)
		if !ok {
			continue
		}
		if download.URL() == "" {
			continue
		}
		return platform, download, true
	}
	return "", hangarDownload{}, false
}

// candidateDependencies maps Hangar's plugin dependency entries to upstream
// project identities. Entries hosted outside Hangar, or without a project ID,
// cannot be resolved through the Hangar catalogue; any such entry makes the
// declaration incomplete so resolution reports the gap.
func (v *hangarVersion) candidateDependencies(
	platformKey string,
) ([]upstream.ProjectDependency, bool) {
	declared, known := v.platformPluginDependencies(platformKey)
	dependencies := make([]upstream.ProjectDependency, 0, len(declared))
	complete := known
	for _, dependency := range declared {
		if dependency.Name == "" ||
			dependency.ProjectID == nil ||
			*dependency.ProjectID <= 0 {
			complete = false
			continue
		}
		if dependency.ExternalURL != nil && *dependency.ExternalURL != "" {
			complete = false
			continue
		}
		kind := catalog.DependencyKindOptional
		if dependency.Required {
			kind = catalog.DependencyKindRequired
		}
		dependencies = append(dependencies, upstream.ProjectDependency{
			Provider:  catalog.ProviderHangar,
			ProjectID: strconv.FormatInt(*dependency.ProjectID, 10),
			Kind:      kind,
		})
	}
	return dependencies, complete
}

func containsMinecraft(declared []string, minecraft string) bool {
	for _, supported := range declared {
		if strings.EqualFold(strings.TrimSpace(supported), minecraft) {
			return true
		}
	}
	return false
}
