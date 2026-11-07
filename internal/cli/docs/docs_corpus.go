package docs

import (
	"fmt"
	"path"
	"strings"

	docscontent "github.com/mclucy/lucy/docs"
)

func findPage(pages []docscontent.Page, slug string) (docscontent.Page, bool) {
	for _, candidate := range pages {
		if candidate.Slug == slug {
			return candidate, true
		}
	}
	return docscontent.Page{}, false
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
