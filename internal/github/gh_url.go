package github

import (
	"net/url"
	"strings"
)

// ProxiedDownloadURL prefixes a release asset URL with proxy. A proxy without
// a trailing slash still separates the asset URL from its prefix. The
// canonical URL is returned when no proxy is configured or rawURL is not an
// HTTPS URL hosted on github.com under /owner/repository/releases/download/.
func ProxiedDownloadURL(rawURL, proxy string) string {
	if proxy == "" || !isReleaseAssetURL(rawURL) {
		return rawURL
	}
	if !strings.HasSuffix(proxy, "/") {
		proxy += "/"
	}
	return proxy + rawURL
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
