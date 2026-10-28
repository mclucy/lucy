//go:build docsarchive

package docs

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	_ "embed"
	"fmt"
	"io"
)

//go:embed docs.tar.gz
var archive []byte

// Load returns the packed pages sorted by slug.
func Load() ([]Page, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, fmt.Errorf("read documentation archive: %w", err)
	}
	defer gz.Close()

	var pages []Page
	tr := tar.NewReader(gz)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read documentation archive: %w", err)
		}
		slug, ok := pageName(header.Name)
		if header.Typeflag != tar.TypeReg || !ok {
			continue
		}
		content, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read documentation archive: %w", err)
		}
		pages = append(pages, Page{Slug: slug, Content: content})
	}
	sortPages(pages)
	return pages, nil
}
