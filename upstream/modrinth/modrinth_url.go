package modrinth

import (
	"net/url"
	"strings"
	"text/template"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/types"
)

const projectUrlPrefix = "https://api.modrinth.com/v2/project/"

func versionsUrl(slug types.PackageName) (urlString string) {
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
	query types.PackageName,
	option searchOptions,
) (urlString string) {
	urlTemplate, _ := template.New("modrinth_search_url").Parse(searchUrlTemplate)
	urlBuilder := strings.Builder{}
	err := urlTemplate.Execute(
		&urlBuilder,
		map[string]any{
			"query":  url.QueryEscape(string(query)),
			"index":  option.index,
			"facets": url.QueryEscape(serializeFacet(option.facets...)),
		},
	)
	if err != nil {
		logger.Error(err)
	}

	urlString = urlBuilder.String()
	return urlString
}

const userHomepageUrlPrefix = `https://modrinth.com/user/`

// userHomepageUrl's suffix is the user's username or id.
func userHomepageUrl(suffix string) (urlString string) {
	return userHomepageUrlPrefix + suffix
}
