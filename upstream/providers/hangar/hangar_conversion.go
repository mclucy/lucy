package hangar

import (
	"slices"
	"sort"
	"strings"

	"github.com/mclucy/lucy/input"
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

func (r hangarProjectRef) CanonicalName() types.BarePackageName {
	return input.ToProjectName(r.Slug)
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

func (s *projectSearchResponse) ToSearchResults(source types.SourceId) upstream.SearchResponse {
	res := upstream.SearchResponse{
		Source: source,
		Items:  make([]upstream.RemotePackageName, 0, len(s.Result)),
	}

	for _, project := range s.Result {
		res.Items = append(
			res.Items, upstream.RemotePackageName{
				RemoteName: project.ProjectRef().CanonicalName().String(),
				Source:     source,
			},
		)
	}

	return res
}

func (p *hangarProject) ToProjectInformation() types.Metadata {
	info := types.Metadata{
		Title:       p.Name,
		Brief:       p.Description,
		Description: p.MainPageContent,
		DescriptionIsMarkdown: p.MainPageContent != "" && (upstream.LooksLikeMarkdown(p.MainPageContent) || strings.Contains(
			p.MainPageContent,
			"#",
		)),
		License: firstNonEmpty(
			p.Settings.License.Name,
			p.Settings.License.Type,
		),
		Authors: make([]types.Person, 0, len(p.MemberNames)),
		Urls:    make([]types.Url, 0, len(p.Settings.Links)+1),
	}

	if projectURL := p.ProjectRef().ProjectURL(); projectURL != "" {
		info.Urls = append(
			info.Urls, types.Url{
				Name: "Hangar",
				Type: types.UrlHome,
				Url:  projectURL,
			},
		)
	}

	for _, memberName := range p.MemberNames {
		info.Authors = append(info.Authors, types.Person{Name: memberName})
	}

	for _, section := range p.Settings.Links {
		for _, link := range section.Links {
			if link.URL == "" {
				continue
			}
			info.Urls = append(
				info.Urls, types.Url{
					Name: link.Name,
					Type: classifyHangarURL(link.Name),
					Url:  link.URL,
				},
			)
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
	remote, _ := v.ToPackageRemoteForPlatform(preferredDownloadPlatform(types.PlatformNone))
	if remote.FileUrl == "" {
		platforms := sortedMapKeys(v.Downloads)
		if len(platforms) == 0 {
			return types.PackageRemote{Source: types.SourceHangar}
		}

		remote, _ = v.ToPackageRemoteForPlatform(types.PlatformId(strings.ToLower(platforms[0])))
	}
	return remote
}

func (v *hangarVersion) ToPackageRemoteForPlatform(platform types.PlatformId) (
	types.PackageRemote,
	bool,
) {
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

func (v *hangarVersion) PluginDependencyNames() []types.BarePackageName {
	depsForPlatform := v.DependenciesForPlatform(types.PlatformNone)
	if len(depsForPlatform) == 0 {
		return nil
	}

	deps := make([]types.BarePackageName, 0, len(depsForPlatform))
	for _, dep := range depsForPlatform {
		if dep.Name == "" || dep.ExternalURL != nil {
			continue
		}
		deps = append(deps, input.ToProjectName(dep.Name))
	}
	slices.Sort(deps)
	return deps
}

func (v *hangarVersion) DependenciesForPlatform(platform types.PlatformId) []hangarPluginDependency {
	if len(v.PluginDependencies) == 0 {
		return nil
	}

	preferredKey := strings.ToUpper(preferredDownloadPlatform(platform).String())
	if deps := v.PluginDependencies[preferredKey]; len(deps) > 0 {
		return deps
	}

	for _, key := range sortedMapKeys(v.PluginDependencies) {
		if deps := v.PluginDependencies[key]; len(deps) > 0 {
			return deps
		}
	}

	return nil
}

func (v *hangarVersion) HasDownloadForPlatform(platform types.PlatformId) bool {
	_, ok := v.downloadForPlatform(preferredDownloadPlatform(platform))
	if ok {
		return true
	}
	return len(v.Downloads) > 0 && platform == types.PlatformNone
}

func (v *hangarVersion) SupportsPlatform(platform types.PlatformId) bool {
	if len(v.PlatformDependencies) == 0 {
		return false
	}

	preferredKey := strings.ToUpper(preferredDownloadPlatform(platform).String())
	if versions := v.PlatformDependencies[preferredKey]; len(versions) > 0 {
		return true
	}

	return platform == types.PlatformNone && len(v.PlatformDependencies) > 0
}

func (v *hangarVersion) downloadForPlatform(platform types.PlatformId) (
	hangarDownload,
	bool,
) {
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
		MinecraftVersions: make([]types.BareVersion, 0),
		Platforms:         make([]types.PlatformId, 0, len(platformVersions)),
		Authentic:         true,
	}

	seenVersions := make(map[string]struct{})
	for _, platformKey := range sortedMapKeys(platformVersions) {
		support.Platforms = append(
			support.Platforms,
			types.PlatformId(strings.ToLower(platformKey)),
		)
		for _, version := range platformVersions[platformKey] {
			if _, exists := seenVersions[version]; exists {
				continue
			}
			seenVersions[version] = struct{}{}
			support.MinecraftVersions = append(
				support.MinecraftVersions,
				types.BareVersion(version),
			)
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
