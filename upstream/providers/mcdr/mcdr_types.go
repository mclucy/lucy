package mcdr

import (
	"time"

	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/types"
)

// SourceGitHub API file ref: https://api.github.com/repos/MCDReforged/PluginCatalogue/contents/plugins/{plugin_name}/plugin_info.json
// Lucy uses this file only for authorship; metadata comes from meta.json on
// the meta branch instead.
type pluginInfo struct {
	ID           string   `json:"id"`
	Authors      []author `json:"authors"`
	Repository   string   `json:"repository"`
	Branch       string   `json:"branch"`
	RelatedPath  string   `json:"related_path"`
	Labels       []string `json:"labels"`
	Introduction struct {
		EnUs string `json:"en_us"`
		ZhCn string `json:"zh_cn"`
	} `json:"introduction"`
}

type author struct {
	Name string `json:"name"`
	Link string `json:"link"`
}

// SourceGitHub API file ref: https://api.github.com/repos/MCDReforged/PluginCatalogue/{plugin_name}/release.json?ref=meta
type pluginRelease struct {
	SchemaVersion      int       `json:"schema_version"`
	ID                 string    `json:"id"`
	LatestVersion      string    `json:"latest_version"`
	LatestVersionIndex int       `json:"latest_version_index"`
	Releases           []release `json:"releases"`
}
type release struct {
	URL         string     `json:"url"`
	Name        string     `json:"name"`
	TagName     string     `json:"tag_name"`
	CreatedAt   time.Time  `json:"created_at"`
	Description string     `json:"description"`
	Prerelease  bool       `json:"prerelease"`
	Asset       asset      `json:"asset"`
	Meta        pluginMeta `json:"meta"`
}

func (r release) ToPackageRemote() types.ResolvedPackage {
	remote := types.ResolvedPackage{
		ID:       types.VersionedPackageRef{PackageRef: types.PackageRef{Provider: types.ProviderMCDR}},
		FileURL:  r.Asset.BrowserDownloadUrl,
		Filename: r.Asset.Name,
	}
	return remote
}

type asset struct {
	ID                 int       `json:"id"`
	Name               string    `json:"name"`
	Size               int       `json:"size"`
	DownloadCount      int       `json:"download_count"`
	CreatedAt          time.Time `json:"created_at"`
	BrowserDownloadUrl string    `json:"browser_download_url"`
	HashMd5            string    `json:"hash_md5"`
	HashSha256         string    `json:"hash_sha256"`
}

// SourceGitHub API file ref: https://api.github.com/repos/MCDReforged/PluginCatalogue/contents/{plugin_name}/meta.json?ref=meta
type pluginMeta struct {
	SchemaVersion int               `json:"schema_version"`
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Version       string            `json:"version"`
	Link          string            `json:"link"`
	Authors       []string          `json:"authors"`
	Dependencies  map[string]string `json:"dependencies"`
	Requirements  []string          `json:"requirements"`
	Description   struct {
		EnUs string `json:"en_us"`
		ZhCn string `json:"zh_cn"`
	} `json:"description"`
}

// SourceGitHub API file ref: https://api.github.com/repos/MCDReforged/PluginCatalogue/contents/{plugin_name}/repository.json?ref=meta
type pluginRepo struct {
	URL             string `json:"url"`
	Name            string `json:"name"`
	FullName        string `json:"full_name"`
	HtmlUrl         string `json:"html_url"`
	Description     string `json:"description"`
	Archived        bool   `json:"archived"`
	StargazersCount int    `json:"stargazers_count"`
	WatchersCount   int    `json:"watchers_count"`
	ForksCount      int    `json:"forks_count"`
	Readme          string `json:"readme"`
	ReadmeUrl       string `json:"readme_url"`
	License         *struct {
		Key    string `json:"key"`
		Name   string `json:"name"`
		SpdxId string `json:"spdx_id"`
		URL    string `json:"url"`
	} `json:"license"`
}

type rawProjectInformation struct {
	Info       *pluginInfo
	Meta       *pluginMeta
	Repository *pluginRepo
}

func (r rawProjectInformation) ToProjectInformation() types.Metadata {
	info := types.Metadata{
		Title:                 r.Meta.Name,
		Brief:                 r.Meta.Description.EnUs,
		Description:           r.Repository.Readme,
		DescriptionURL:        r.Repository.HtmlUrl,
		DescriptionIsMarkdown: true,
		Authors:               nil,
		URLs:                  nil,
		License: fn.Ternary(
			r.Repository.License != nil,
			r.Repository.License.Name,
			"n/a",
		),
	}

	info.Authors = make([]types.Person, 0)
	for _, author := range r.Info.Authors {
		info.Authors = append(
			info.Authors, types.Person{
				Name: author.Name,
				URL:  author.Link,
			},
		)
	}

	info.URLs = make([]types.URL, 0)
	info.URLs = append(
		info.URLs,
		types.URL{
			Name: "Plugin Page",
			Type: types.URLHome,
			URL:  r.Meta.Link,
		}, types.URL{
			Name: "SourceGitHub Repo",
			Type: types.URLSource,
			URL:  r.Info.Repository,
		},
	)

	return info
}
