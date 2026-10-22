package curseforge

import (
	"context"
	"fmt"
	"strconv"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
)

func (provider) Candidates(ctx context.Context, project, minecraft string, loader catalog.Ecosystem) ([]upstream.Candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	id, err := strconv.ParseInt(project, 10, 32)
	if err != nil {
		mod, e := resolveSlug(catalog.Name(project))
		if e != nil {
			return nil, e
		}
		id = int64(mod.ID)
	}
	// Pagination is required: exact older files are not necessarily on page one.
	var files []fileResponse
	for offset := 0; ; offset += 50 {
		endpoint := modFilesUrl(int32(id), minecraft, modLoaderType(loader)) + "&index=" + strconv.Itoa(offset)
		var page filesResponse
		if err := get(endpoint, &page); err != nil {
			return nil, err
		}
		files = append(files, page.Data...)
		if len(page.Data) < 50 {
			break
		}
	}
	var out []upstream.Candidate
	for _, f := range files {
		if f.DownloadUrl == nil {
			continue
		}
		hashes := map[string]string{}
		for _, h := range f.Hashes {
			if h.Algo == 1 {
				hashes["sha1"] = h.Digest
			}
		}
		c := upstream.Candidate{
			Provider:     catalog.ProviderCurseForge,
			ProjectID:    strconv.FormatInt(id, 10),
			ReleaseID:    strconv.Itoa(int(f.ID)),
			FileID:       strconv.Itoa(int(f.ID)),
			Version:      version.Version(f.DisplayName),
			Filename:     f.FileName,
			URL:          *f.DownloadUrl,
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
		out = append(out, c)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("curseforge project %s has no downloadable %s release for Minecraft %s", project, loader, minecraft)
	}
	return out, nil
}
