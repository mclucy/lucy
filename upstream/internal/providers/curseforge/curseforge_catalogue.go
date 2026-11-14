package curseforge

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
)

func (p *Provider) Candidates(ctx context.Context, project, minecraft string, loader catalog.Ecosystem) ([]upstream.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id, err := p.resolveProjectID(ctx, project)
	if err != nil {
		return nil, err
	}
	// Pagination is required: exact older files are not necessarily on page one.
	// A Quilt request accepts both Quilt and Fabric files, so each loader
	// filter is listed and the union is deduplicated by file identity.
	var files []fileResponse
	seen := make(map[int32]bool)
	for _, loaderType := range modLoaderTypes(loader) {
		page, err := p.listAllFiles(ctx, id, minecraft, loaderType)
		if err != nil {
			return nil, err
		}
		for _, f := range page {
			if seen[f.ID] {
				continue
			}
			seen[f.ID] = true
			files = append(files, f)
		}
	}
	var out []upstream.Candidate
	for _, f := range files {
		if f.DownloadUrl == nil {
			continue
		}
		out = append(out, candidateFromCurseForgeFile(id, f))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("curseforge project %s has no downloadable %s release for Minecraft %s", project, loader, minecraft)
	}
	return out, nil
}

// candidateFromCurseForgeFile converts one CurseForge file record into a
// candidate. The file is the primary file of its release. A null downloadUrl
// yields an empty URL; callers that need a downloadable candidate must skip
// such files beforehand.
func candidateFromCurseForgeFile(
	modID int32,
	f fileResponse,
) upstream.Candidate {
	downloadURL := ""
	if f.DownloadUrl != nil {
		downloadURL = *f.DownloadUrl
	}
	hashes := map[string]string{}
	for _, h := range f.Hashes {
		if h.Algo == 1 {
			hashes["sha1"] = h.Digest
		}
	}
	c := upstream.Candidate{
		Provider:     catalog.ProviderCurseForge,
		ProjectID:    strconv.FormatInt(int64(modID), 10),
		ReleaseID:    strconv.Itoa(int(f.ID)),
		FileID:       strconv.Itoa(int(f.ID)),
		Version:      version.Version(f.DisplayName),
		Filename:     f.FileName,
		URL:          downloadURL,
		Hashes:       hashes,
		Channel:      curseForgeChannel(f.ChannelCode),
		PublishedAt:  parseCurseforgeDate(f.FileDate),
		PrimaryFile:  true,
		Dependencies: []upstream.ProjectDependency{},
	}
	for _, d := range f.Dependencies {
		var kind catalog.DependencyKind
		switch d.RelationType {
		case 3:
			kind = catalog.DependencyKindRequired
		case 2:
			kind = catalog.DependencyKindOptional
		case 5:
			kind = catalog.DependencyKindIncompatible
		}
		if kind != catalog.DependencyKindUnknown {
			c.Dependencies = append(c.Dependencies, upstream.ProjectDependency{
				Provider:  catalog.ProviderCurseForge,
				ProjectID: strconv.Itoa(int(d.ModId)),
				Kind:      kind,
			})
		}
	}
	return c
}
