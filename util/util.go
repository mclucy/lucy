// Package util is a general package for network and file system operations.
package util

import (
	"mime"
	"net/http"
	"net/url"
	"strings"
)

const (
	ProgramPath = ".lucy"
	ConfigFile  = ProgramPath + "/config.json"
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

	segments := strings.Split(u.Path, "/")
	if len(segments) == 0 {
		return ""
	}

	filename := segments[len(segments)-1]

	return filename
}
