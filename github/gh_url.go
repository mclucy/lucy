package github

import (
	"net/url"
	"strings"
)

const mirrorBaseURL = "https://ghfast.top/"

// DownloadURL returns the URL to request for a GitHub release asset.
// The original URL is returned unless useMirror is enabled and rawURL is an
// HTTPS URL hosted on github.com under /owner/repository/releases/download/.
func DownloadURL(rawURL string, useMirror bool) string {
	if !useMirror || !isReleaseAssetURL(rawURL) {
		return rawURL
	}
	return mirrorBaseURL + rawURL
}

func isReleaseAssetURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "github.com") {
		return false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	return len(parts) >= 6 &&
		parts[0] != "" &&
		parts[1] != "" &&
		parts[2] == "releases" &&
		parts[3] == "download" &&
		parts[4] != "" &&
		parts[5] != ""
}
