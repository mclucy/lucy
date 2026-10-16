package cache

import (
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
)

func speculateFilename(resp *http.Response) string {
	if filename, ok := getFilenameFromHeader(resp); ok {
		return filename
	}
	filename := getFilenameFromURL(resp.Request.URL.String())
	return filename
}

func getFilenameFromHeader(resp *http.Response) (string, bool) {
	contentDisposition := resp.Header.Get("Content-Disposition")
	if contentDisposition == "" {
		return "", false
	}

	_, params, err := mime.ParseMediaType(contentDisposition)
	if err != nil {
		return "", false
	}

	filename, ok := params["filename"]
	return filename, ok
}

func getFilenameFromURL(urlString string) string {
	u, err := url.Parse(urlString)
	if err != nil {
		return ""
	}

	_, filename, found := strings.CutLast(u.Path, "/")
	if !found {
		return u.Path
	}
	return filename
}

// sanitizeFilename prevents path traversal by stripping directory components.
func sanitizeFilename(name, fallback string) string {
	name = filepath.Base(name)
	if !filepath.IsLocal(name) || name == "." || name == ".." || name == "/" || name == string(filepath.Separator) {
		return fallback
	}
	return name
}

// containedUnder reports whether child is strictly inside parent.
func containedUnder(parent, child string) bool {
	absParent, err := filepath.Abs(parent)
	if err != nil {
		return false
	}
	absChild, err := filepath.Abs(child)
	if err != nil {
		return false
	}
	prefix := absParent
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	return strings.HasPrefix(absChild, prefix)
}
