// Package docs supplies the documentation pages embedded in lucy.
package docs

import (
	"cmp"
	"slices"
	"strings"
)

// Page is one documentation page. Slug is the page path without the .mdx suffix.
type Page struct {
	Slug    string
	Content []byte
}

// pageName reports the slug of a page path. Drafts are not pages.
func pageName(name string) (string, bool) {
	if !strings.HasSuffix(name, ".mdx") || strings.HasSuffix(name, ".draft.mdx") {
		return "", false
	}
	return strings.TrimSuffix(name, ".mdx"), true
}

func sortPages(pages []Page) {
	slices.SortFunc(pages, func(a, b Page) int {
		return cmp.Compare(a.Slug, b.Slug)
	})
}
