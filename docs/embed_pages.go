//go:build !docsarchive

package docs

import (
	"embed"
	"io/fs"
)

// Sources are the tracked page files, read directly by builds that do not pack
// the documentation archive.
//
//go:embed *.mdx agents/*.mdx commands/*.mdx file-tracking/*.mdx
var sources embed.FS

// Load returns the embedded pages sorted by slug.
func Load() ([]Page, error) {
	var pages []Page
	err := fs.WalkDir(sources, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		slug, ok := pageName(name)
		if !ok {
			return nil
		}
		content, err := sources.ReadFile(name)
		if err != nil {
			return err
		}
		pages = append(pages, Page{Slug: slug, Content: content})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sortPages(pages)
	return pages, nil
}
