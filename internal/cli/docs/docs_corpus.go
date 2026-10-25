package docs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	docscontent "github.com/mclucy/lucy/docs"
)

type page struct {
	slug    string
	content []byte
}

func loadPages() ([]page, error) {
	gz, err := gzip.NewReader(bytes.NewReader(docscontent.Archive))
	if err != nil {
		return nil, err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	pages := make([]page, 0, 18)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			return nil, err
		}
		pages = append(pages, page{
			slug:    strings.TrimSuffix(header.Name, ".mdx"),
			content: content,
		})
	}
	sort.Slice(pages, func(i, j int) bool {
		return pages[i].slug < pages[j].slug
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
