package modrinth

import (
	"context"
	"fmt"
	"slices"

	"github.com/mclucy/lucy/types"
)

func (provider) Candidates(ctx context.Context, project, minecraft string, loader types.Ecosystem) ([]types.CatalogueCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	versions, err := listVersions(types.PackageName(project))
	if err != nil {
		return nil, err
	}
	var out []types.CatalogueCandidate
	for _, v := range versions {
		if !slices.Contains(v.GameVersions, minecraft) || !versionSupportsLoader(v, loader) {
			continue
		}
		// Modrinth infers the first file as primary when no file carries the
		// flag, so a version without a marked primary still must produce a
		// primary candidate instead of losing every candidate to the check
		// in resolve.
		unmarked := !slices.ContainsFunc(v.Files, func(f fileResponse) bool { return f.IsPrimary })
		for i, f := range v.Files {
			if f.FileType != "" {
				continue
			}
			hashes := map[string]string{}
			if f.Hashes.Sha512 != "" {
				hashes["sha512"] = f.Hashes.Sha512
			}
			if f.Hashes.Sha1 != "" {
				hashes["sha1"] = f.Hashes.Sha1
			}
			c := types.CatalogueCandidate{
				Provider:     types.ProviderModrinth,
				ProjectID:    v.ProjectId,
				ReleaseID:    v.ID,
				FileID:       f.ID,
				Version:      types.Version(v.VersionNumber),
				Filename:     f.Filename,
				URL:          f.URL,
				Hashes:       hashes,
				Channel:      types.ParseReleaseChannel(v.VersionType),
				PublishedAt:  v.DatePublished,
				PrimaryFile:  f.IsPrimary || (unmarked && i == 0),
				Dependencies: []types.ProjectDependency{},
			}
			for _, d := range v.Dependencies {
				projectID := d.ProjectId
				if projectID == "" && d.VersionId != "" {
					pinned, e := getVersionById(d.VersionId)
					if e != nil {
						return nil, e
					}
					projectID = pinned.ProjectId
				}
				if projectID != "" {
					c.Dependencies = append(c.Dependencies, types.ProjectDependency{
						Provider:  types.ProviderModrinth,
						ProjectID: projectID,
						ReleaseID: d.VersionId,
						Kind:      types.ParseDependencyKind(string(d.DependencyType)),
					})
				}
			}
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("modrinth project %s has no %s release for Minecraft %s", project, loader, minecraft)
	}
	return out, nil
}
