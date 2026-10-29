package docs

import (
	"fmt"
	"path"
	"strings"

	docscontent "github.com/mclucy/lucy/docs"
)

type page struct {
	slug    string
	content []byte
}

func loadPages() ([]page, error) {
	corpus, err := docscontent.Load()
	if err != nil {
		return nil, err
	}
	pages := make([]page, len(corpus))
	for i, entry := range corpus {
		pages[i] = page{slug: entry.Slug, content: entry.Content}
	}
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
