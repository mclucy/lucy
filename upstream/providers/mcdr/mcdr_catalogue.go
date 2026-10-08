package mcdr

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mclucy/lucy/types"
)

func (provider) Candidates(ctx context.Context, project, _ string, loader types.Ecosystem) ([]types.CatalogueCandidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if loader != types.EcoMcdr {
		return nil, fmt.Errorf("MCDR catalogue requires the mcdr loader")
	}
	history, err := getReleaseHistory(project)
	if err != nil {
		return nil, err
	}
	out := make([]types.CatalogueCandidate, 0, len(history.Releases))
	for _, rel := range history.Releases {
		hashes := map[string]string{}
		if rel.Asset.HashSha256 != "" {
			hashes["sha256"] = rel.Asset.HashSha256
		}
		kind := "release"
		if rel.Prerelease {
			kind = "beta"
		}
		c := types.CatalogueCandidate{Artifact: types.Artifact{Provider: "mcdr", ProjectID: history.Id, ReleaseID: rel.TagName, FileID: strconv.Itoa(rel.Asset.Id), Version: rel.Meta.Version, Filename: rel.Asset.Name, URL: rel.Asset.BrowserDownloadUrl, Hashes: hashes}, ReleaseType: kind, Published: rel.CreatedAt.Format("2006-01-02T15:04:05Z07:00"), Primary: true, Dependencies: []types.ProjectDependency{}}
		if c.ProjectID == "" {
			c.ProjectID = project
		}
		for id := range rel.Meta.Dependencies {
			if id != "mcdreforged" {
				c.Dependencies = append(c.Dependencies, types.ProjectDependency{Provider: "mcdr", ProjectID: id, Kind: "required"})
			}
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("MCDR catalogue project %s has no releases", project)
	}
	return out, nil
}
