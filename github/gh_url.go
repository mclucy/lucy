package github

import (
	"net/url"
	"strings"
	"sync"
)

var (
	proxyMutex   sync.RWMutex
	releaseProxy string
)

// SetReleaseProxy configures the prefix that mirrors GitHub release assets.
// An empty prefix downloads from the canonical URL.
func SetReleaseProxy(prefix string) {
	proxyMutex.Lock()
	releaseProxy = strings.TrimSpace(prefix)
	proxyMutex.Unlock()
}

// DownloadURL returns the URL to request for a GitHub release asset. The
// canonical URL is returned when no release proxy is configured or rawURL is
// not an HTTPS URL hosted on github.com under /owner/repository/releases/download/.
func DownloadURL(rawURL string) string {
	proxyMutex.RLock()
	prefix := releaseProxy
	proxyMutex.RUnlock()
	return ProxiedDownloadURL(rawURL, prefix)
}

// ProxiedDownloadURL prefixes a release asset URL with proxy. A proxy without
// a trailing slash still separates the asset URL from its prefix.
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
