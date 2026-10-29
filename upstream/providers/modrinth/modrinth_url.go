package modrinth

import (
	"net/url"
	"strings"
	"text/template"

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

func projectMemberUrl(suffix string) (urlString string) {
	return projectUrl(suffix) + "/members"
}

func projectDependencyUrl(suffix string) (urlString string) {
	return projectUrl(suffix) + "/dependencies"
}

const searchUrlTemplate = `https://api.modrinth.com/v2/search?query={{.query}}&limit=100&index={{.index}}&facets={{.facets}}`

func searchUrl(
	query string,
	option searchOptions,
) (urlString string) {
	urlTemplate, _ := template.New("modrinth_search_url").Parse(searchUrlTemplate)
	urlBuilder := strings.Builder{}
	// The template is a static constant; execution only fails on a writer
	// error, which strings.Builder cannot produce.
	_ = urlTemplate.Execute(
		&urlBuilder,
		map[string]any{
			"query":  url.QueryEscape(query),
			"index":  option.index,
			"facets": url.QueryEscape(serializeFacet(option.facets...)),
		},
	)

	urlString = urlBuilder.String()
	return urlString
}

const userHomepageUrlPrefix = `https://modrinth.com/user/`

// userHomepageUrl's suffix is the user's username or id.
func userHomepageUrl(suffix string) (urlString string) {
	return userHomepageUrlPrefix + suffix
}
