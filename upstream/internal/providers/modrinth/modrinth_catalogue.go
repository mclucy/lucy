package modrinth

import (
	"context"
	"fmt"
	"slices"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
)

func (p *Provider) Candidates(ctx context.Context, project, minecraft string, loader catalog.Ecosystem) ([]upstream.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	versions, err := p.listVersions(ctx, catalog.Name(project))
	if err != nil {
		return nil, err
	}
	var out []upstream.Candidate
	for _, v := range versions {
		if v.Status != "" && v.Status != "listed" {
			continue
		}
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
			c, err := p.candidateFromVersion(ctx, v, f, f.IsPrimary || (unmarked && i == 0))
			if err != nil {
				return nil, err
			}
			out = append(out, c)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("modrinth project %s has no %s release for Minecraft %s", project, loader, minecraft)
	}
	return out, nil
}

// candidateFromVersion builds the candidate for one file of a version. The
// caller owns the primary flag; the helper resolves a dependency whose
// project is carried by its version through the version endpoint.
func (p *Provider) candidateFromVersion(
	ctx context.Context,
	v *versionResponse,
	f fileResponse,
	primary bool,
) (upstream.Candidate, error) {
	hashes := map[string]string{}
	if f.Hashes.Sha512 != "" {
		hashes["sha512"] = f.Hashes.Sha512
	}
	if f.Hashes.Sha1 != "" {
		hashes["sha1"] = f.Hashes.Sha1
	}
	c := upstream.Candidate{
		Provider:     catalog.ProviderModrinth,
		ProjectID:    v.ProjectId,
		ReleaseID:    v.ID,
		FileID:       f.ID,
		Version:      version.Version(v.VersionNumber),
		Filename:     f.Filename,
		URL:          f.URL,
		Hashes:       hashes,
		Channel:      upstream.ParseReleaseChannel(v.VersionType),
		PublishedAt:  v.DatePublished,
		PrimaryFile:  primary,
		Dependencies: []upstream.ProjectDependency{},
	}
	for _, d := range v.Dependencies {
		projectID := d.ProjectId
		if projectID == "" && d.VersionId != "" {
			pinned, e := p.getVersionById(ctx, d.VersionId)
			if e != nil {
				return upstream.Candidate{}, e
			}
			projectID = pinned.ProjectId
		}
		if projectID != "" {
			c.Dependencies = append(c.Dependencies, upstream.ProjectDependency{
				Provider:  catalog.ProviderModrinth,
				ProjectID: projectID,
				ReleaseID: d.VersionId,
				Kind:      catalog.ParseDependencyKind(string(d.DependencyType)),
			})
		}
	}
	return c, nil
}
