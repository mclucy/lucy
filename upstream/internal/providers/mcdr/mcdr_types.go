package mcdr

import (
	"time"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
)

// pluginInfo supplies authorship and the source repository URL from the
// "plugin" section of
// https://api.mcdreforged.com/catalogue/{plugin_id}/all.json.
// Metadata comes from the "meta" section instead.
type pluginInfo struct {
	ID           string   `json:"id"`
	Authors      []string `json:"authors"`
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

// authorSummary is the catalogue-wide author index from
// https://api.mcdreforged.com/catalogue/authors.json. It maps each author name
// to the record the catalogue collected from plugin_info.json, keeping the
// author links the aggregate omits.
type authorSummary struct {
	Amount  int               `json:"amount"`
	Authors map[string]author `json:"authors"`
}

// pluginAll is the official per-plugin catalogue aggregate from
// https://api.mcdreforged.com/catalogue/{plugin_id}/all.json. The catalogue
// documents meta, release, and repository as null when refreshing that section
// failed, so Info treats those sections as optional. The release section is
// not decoded here: release history uses the narrower release.json document.
type pluginAll struct {
	Meta       *pluginMeta `json:"meta"`
	Plugin     *pluginInfo `json:"plugin"`
	Repository *pluginRepo `json:"repository"`
}

// pluginRelease contains releases from https://api.mcdreforged.com/catalogue/{plugin_id}/release.json.
type pluginRelease struct {
	SchemaVersion int       `json:"schema_version"`
	ID            string    `json:"id"`
	Releases      []release `json:"releases"`
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

// Channel reports the stability channel from the release's prerelease flag.
func (r release) Channel() upstream.ReleaseChannel {
	if r.Prerelease {
		return upstream.ReleaseChannelBeta
	}
	return upstream.ReleaseChannelRelease
}

// ToBuild maps a catalogue release to its MCDR compatibility.
func (r release) ToBuild() upstream.Build {
	return upstream.Build{
		Version:   r.Meta.Version,
		Loaders:   []string{"mcdr"},
		Channel:   string(r.Channel()),
		Published: r.CreatedAt,
	}
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

// pluginMeta contains the "meta" section of https://api.mcdreforged.com/catalogue/{plugin_id}/all.json.
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

// pluginRepo contains the "repository" section of https://api.mcdreforged.com/catalogue/{plugin_id}/all.json.
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
	Info        *pluginInfo
	Meta        *pluginMeta
	Repository  *pluginRepo
	AuthorLinks map[string]string
}

func (r rawProjectInformation) ToProjectInformation() catalog.Metadata {
	info := catalog.Metadata{
		DescriptionIsMarkdown: true,
		Authors:               make([]catalog.Person, 0),
		URLs:                  make([]catalog.URL, 0),
		License:               "n/a",
	}

	if r.Meta != nil {
		info.Title = r.Meta.Name
		info.Brief = r.Meta.Description.EnUs
		if r.Meta.Link != "" {
			info.URLs = append(info.URLs, catalog.URL{
				Name: "Plugin Page",
				Type: catalog.URLHome,
				URL:  r.Meta.Link,
			})
		}
	}

	if r.Repository != nil {
		info.Description = r.Repository.Readme
		info.DescriptionURL = r.Repository.HtmlUrl
		if r.Repository.License != nil {
			info.License = r.Repository.License.Name
		}
	}

	if r.Info != nil {
		for _, name := range r.Info.Authors {
			if name == "" {
				continue
			}
			info.Authors = append(
				info.Authors, catalog.Person{
					Name: name,
					URL:  r.AuthorLinks[name],
				},
			)
		}
		if r.Info.Repository != "" {
			info.URLs = append(info.URLs, catalog.URL{
				Name: "SourceGitHub Repo",
				Type: catalog.URLSource,
				URL:  r.Info.Repository,
			})
		}
	}

	return info
}
