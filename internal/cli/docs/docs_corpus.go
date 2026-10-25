package docs

import (
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"

	docscontent "github.com/mclucy/lucy/docs"
)

type page struct {
	slug    string
	content []byte
}

func loadPages() ([]page, error) {
	pages := make([]page, 0, 18)
	err := fs.WalkDir(
		docscontent.Files,
		".",
		func(filePath string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				return nil
			}
			content, err := fs.ReadFile(docscontent.Files, filePath)
			if err != nil {
				return err
			}
			pages = append(
				pages,
				page{
					slug:    strings.TrimSuffix(filePath, ".mdx"),
					content: content,
				},
			)
			return nil
		},
	)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(pages, func(a, b page) int {
		return strings.Compare(a.slug, b.slug)
	})
	return pages, nil
}

func findPage(pages []page, slug string) (page, bool) {
	for _, candidate := range pages {
		if candidate.slug == slug {
			return candidate, true
		}
	}
	return page{}, false
}

func normalizeSlug(raw string) (string, error) {
	slug := strings.TrimSpace(raw)
	slug = strings.TrimPrefix(slug, "/")
	slug = strings.TrimSuffix(slug, ".mdx")
	slug = path.Clean(slug)
	if slug == "." || slug == ".." || strings.HasPrefix(slug, "../") {
		return "", fmt.Errorf("invalid documentation page %q", raw)
	}
	return slug, nil
}
