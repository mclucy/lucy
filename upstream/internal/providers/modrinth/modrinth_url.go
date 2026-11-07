package modrinth

import (
	"net/url"

	"github.com/mclucy/lucy/catalog"
)

const projectUrlPrefix = "https://api.modrinth.com/v2/project/"

func versionsUrl(slug catalog.Name) (urlString string) {
	urlString, _ = url.JoinPath(
		projectUrlPrefix,
		string(slug),
		"version",
	)
	return
}

const versionUrlPrefix = `https://api.modrinth.com/v2/version/`

func versionUrl(id string) (urlString string) {
	return versionUrlPrefix + url.PathEscape(id)
}

// projectUrl returns the URL for a project with the given SourceModrinth project id
// or slug (package name).
func projectUrl(suffix string) (urlString string) {
	return projectUrlPrefix + url.PathEscape(suffix)
}

const searchUrlPrefix = "https://api.modrinth.com/v2/search?query="

func searchUrl(
	query string,
	option searchOptions,
) string {
	return searchUrlPrefix + url.QueryEscape(query) +
		"&limit=100&index=" + option.index +
		"&facets=" + url.QueryEscape(serializeFacet(option.facets...))
}
