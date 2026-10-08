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
	versions, err := listVersions(types.BarePackageName(project))
	if err != nil {
		return nil, err
	}
	out := []types.CatalogueCandidate{}
	for _, v := range versions {
		if !slices.Contains(v.GameVersions, minecraft) || !versionSupportsLoader(v, loader) {
			continue
		}
		for _, f := range v.Files {
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
			c := types.CatalogueCandidate{Artifact: types.Artifact{Provider: "modrinth", ProjectID: v.ProjectId, ReleaseID: v.Id, FileID: f.Filename, Version: v.VersionNumber, Filename: f.Filename, URL: f.Url, Hashes: hashes}, ReleaseType: v.VersionType, Published: v.DatePublished.Format("2006-01-02T15:04:05Z07:00"), Primary: f.Primary, Dependencies: []types.ProjectDependency{}}
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
					c.Dependencies = append(c.Dependencies, types.ProjectDependency{Provider: "modrinth", ProjectID: projectID, ReleaseID: d.VersionId, Kind: string(d.DependencyType)})
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
