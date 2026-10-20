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
	if loader != types.EcosystemMCDR {
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
		channel := types.ReleaseChannelRelease
		if rel.Prerelease {
			channel = types.ReleaseChannelBeta
		}
		projectID := history.ID
		if projectID == "" {
			projectID = project
		}
		c := types.CatalogueCandidate{
			Provider:     types.ProviderMCDR,
			ProjectID:    projectID,
			ReleaseID:    rel.TagName,
			FileID:       strconv.Itoa(rel.Asset.ID),
			Version:      types.Version(rel.Meta.Version),
			Filename:     rel.Asset.Name,
			URL:          rel.Asset.BrowserDownloadUrl,
			Hashes:       hashes,
			Channel:      channel,
			PublishedAt:  rel.CreatedAt,
			PrimaryFile:  true,
			Dependencies: []types.ProjectDependency{},
		}
		for id := range rel.Meta.Dependencies {
			if id != "mcdreforged" {
				c.Dependencies = append(c.Dependencies, types.ProjectDependency{
					Provider:  types.ProviderMCDR,
					ProjectID: id,
					Kind:      types.DependencyKindRequired,
				})
			}
		}
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("MCDR catalogue project %s has no releases", project)
	}
	return out, nil
}
