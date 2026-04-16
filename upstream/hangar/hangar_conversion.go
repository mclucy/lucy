package hangar

import (
	"sort"
	"strings"

	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

const hangarSiteBaseURL = "https://hangar.papermc.io"

type hangarProjectRef struct {
	Owner string
	Slug  string
}

func (p hangarProject) ProjectRef() hangarProjectRef {
	return p.Namespace.ProjectRef()
}

func (n hangarProjectNamespace) ProjectRef() hangarProjectRef {
	return hangarProjectRef{Owner: n.Owner, Slug: n.Slug}
}

func (r hangarProjectRef) CanonicalName() types.ProjectName {
	return syntax.ToProjectName(r.Slug)
}

func (r hangarProjectRef) LookupPath() string {
	if r.Owner == "" {
		return r.Slug
	}
	return r.Owner + "/" + r.Slug
}

func (r hangarProjectRef) ProjectURL() string {
	if r.Owner == "" || r.Slug == "" {
		return ""
	}
	return hangarSiteBaseURL + "/" + r.Owner + "/" + r.Slug
}

func (s *projectSearchResponse) ToSearchResults() types.SearchResults {
	res := types.SearchResults{
		Source:   types.SourceHangar,
		Projects: make([]types.ProjectName, 0, len(s.Result)),
	}

	for _, project := range s.Result {
		res.Projects = append(res.Projects, project.ProjectRef().CanonicalName())
	}

	return res
}

func (p *hangarProject) ToProjectInformation() types.ProjectInformation {
	info := types.ProjectInformation{
		Title:                 p.Name,
		Brief:                 p.Description,
		Description:           p.MainPageContent,
		DescriptionIsMarkdown: p.MainPageContent != "" && (upstream.LooksLikeMarkdown(p.MainPageContent) || strings.Contains(p.MainPageContent, "#")),
		License:               firstNonEmpty(p.Settings.License.Name, p.Settings.License.Type),
		Authors:               make([]types.Person, 0, len(p.MemberNames)),
		Urls:                  make([]types.Url, 0, len(p.Settings.Links)+1),
	}

	if projectURL := p.ProjectRef().ProjectURL(); projectURL != "" {
		info.Urls = append(info.Urls, types.Url{
			Name: "Hangar",
			Type: types.UrlHome,
			Url:  projectURL,
		})
	}

	for _, memberName := range p.MemberNames {
		info.Authors = append(info.Authors, types.Person{Name: memberName})
	}

	for _, section := range p.Settings.Links {
		for _, link := range section.Links {
			if link.URL == "" {
				continue
			}
			info.Urls = append(info.Urls, types.Url{
				Name: link.Name,
				Type: classifyHangarURL(link.Name),
				Url:  link.URL,
			})
		}
	}

	return info
}

func (p *hangarProject) ToProjectSupport() types.PlatformSupport {
	return platformSupportFromMap(p.SupportedPlatforms)
}

func (v *hangarVersion) ToProjectSupport() types.PlatformSupport {
	return platformSupportFromMap(v.PlatformDependencies)
}

func (v *hangarVersion) ToPackageRemote() types.PackageRemote {
	platforms := sortedMapKeys(v.Downloads)
	if len(platforms) == 0 {
		return types.PackageRemote{Source: types.SourceHangar}
	}

	remote, _ := v.ToPackageRemoteForPlatform(types.Platform(strings.ToLower(platforms[0])))
	return remote
}

func (v *hangarVersion) ToPackageRemoteForPlatform(platform types.Platform) (types.PackageRemote, bool) {
	download, ok := v.downloadForPlatform(platform)
	if !ok {
		return types.PackageRemote{Source: types.SourceHangar}, false
	}

	remote := types.PackageRemote{
		Source:   types.SourceHangar,
		FileUrl:  download.URL(),
		Filename: download.FileInfo.Name,
	}

	if download.FileInfo.SHA256Hash != "" {
		remote.Hash = download.FileInfo.SHA256Hash
		remote.HashAlgorithm = "sha256"
	}

	return remote, true
}

func (v *hangarVersion) PluginDependencyNames() []types.ProjectName {
	if len(v.PluginDependencies) == 0 {
		return nil
	}

	keys := sortedMapKeys(v.PluginDependencies)
	deps := make([]types.ProjectName, 0, len(keys))
	for _, key := range keys {
		deps = append(deps, syntax.ToProjectName(key))
	}
	return deps
}

func (v *hangarVersion) downloadForPlatform(platform types.Platform) (hangarDownload, bool) {
	if len(v.Downloads) == 0 {
		return hangarDownload{}, false
	}

	needle := strings.ToLower(platform.String())
	for key, download := range v.Downloads {
		if strings.ToLower(key) == needle {
			return download, true
		}
	}

	return hangarDownload{}, false
}

func (d hangarDownload) URL() string {
	if d.ExternalURL != nil && *d.ExternalURL != "" {
		return *d.ExternalURL
	}
	return d.DownloadURL
}

func platformSupportFromMap(platformVersions hangarPlatformVersionMap) types.PlatformSupport {
	support := types.PlatformSupport{
		MinecraftVersions: make([]types.RawVersion, 0),
		Platforms:         make([]types.Platform, 0, len(platformVersions)),
		Authentic:         true,
	}

	seenVersions := make(map[string]struct{})
	for _, platformKey := range sortedMapKeys(platformVersions) {
		support.Platforms = append(support.Platforms, types.Platform(strings.ToLower(platformKey)))
		for _, version := range platformVersions[platformKey] {
			if _, exists := seenVersions[version]; exists {
				continue
			}
			seenVersions[version] = struct{}{}
			support.MinecraftVersions = append(support.MinecraftVersions, types.RawVersion(version))
		}
	}

	return support
}

func classifyHangarURL(name string) types.UrlType {
	switch strings.ToLower(name) {
	case "issues":
		return types.UrlIssues
	case "source":
		return types.UrlSource
	case "wiki":
		return types.UrlWiki
	case "support", "discord":
		return types.UrlForum
	case "website", "homepage", "hangar":
		return types.UrlHome
	default:
		return types.UrlMisc
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func sortedMapKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
