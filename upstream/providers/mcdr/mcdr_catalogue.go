package mcdr

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
)

func (provider) Candidates(ctx context.Context, project, _ string, loader catalog.Ecosystem) ([]upstream.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if loader != catalog.EcosystemMCDR {
		return nil, fmt.Errorf("MCDR catalogue requires the mcdr loader")
	}
	history, err := getReleaseHistory(project)
	if err != nil {
		return nil, err
	}
	out := make([]upstream.Candidate, 0, len(history.Releases))
	for _, rel := range history.Releases {
		hashes := map[string]string{}
		if rel.Asset.HashSha256 != "" {
			hashes["sha256"] = rel.Asset.HashSha256
		}
		channel := rel.Channel()
		projectID := history.ID
		if projectID == "" {
			projectID = project
		}
		c := upstream.Candidate{
			Provider:     catalog.ProviderMCDR,
			ProjectID:    projectID,
			ReleaseID:    rel.TagName,
			FileID:       strconv.Itoa(rel.Asset.ID),
			Version:      version.Version(rel.Meta.Version),
			Filename:     rel.Asset.Name,
			URL:          rel.Asset.BrowserDownloadUrl,
			Hashes:       hashes,
			Channel:      channel,
			PublishedAt:  rel.CreatedAt,
			PrimaryFile:  true,
			Dependencies: []upstream.ProjectDependency{},
		}
		for id := range rel.Meta.Dependencies {
			if id != "mcdreforged" {
				c.Dependencies = append(c.Dependencies, upstream.ProjectDependency{
					Provider:  catalog.ProviderMCDR,
					ProjectID: id,
					Kind:      catalog.DependencyKindRequired,
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
