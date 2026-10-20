package hangar

import (
	"maps"
	"slices"
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

func (r hangarProjectRef) CanonicalName() types.PackageName {
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

func (s *projectSearchResponse) ToSearchResults(source types.Provider) upstream.SearchResponse {
	res := upstream.SearchResponse{
		Provider: source,
		Items:    make([]upstream.SearchResult, 0, len(s.Result)),
	}

	for _, project := range s.Result {
		res.Items = append(
			res.Items, upstream.SearchResult{
				ProjectName: project.ProjectRef().CanonicalName().String(),
				Provider:    source,
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
		URLs:    make([]types.URL, 0, len(p.Settings.Links)+1),
	}

	if projectURL := p.ProjectRef().ProjectURL(); projectURL != "" {
		info.URLs = append(
			info.URLs, types.URL{
				Name: "Hangar",
				Type: types.URLHome,
				URL:  projectURL,
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
			info.URLs = append(
				info.URLs, types.URL{
					Name: link.Name,
					Type: classifyHangarURL(link.Name),
					URL:  link.URL,
				},
			)
		}
	}

	return info
}

func (v *hangarVersion) ToPackageRemote() types.ResolvedPackage {
	remote, _ := v.ToPackageRemoteForPlatform(preferredDownloadPlatform(types.EcosystemUnspecified))
	if remote.FileURL == "" {
		platforms := sortedMapKeys(v.Downloads)
		if len(platforms) == 0 {
			return types.ResolvedPackage{ID: types.VersionedPackageRef{PackageRef: types.PackageRef{Provider: types.ProviderHangar}}}
		}

		remote, _ = v.ToPackageRemoteForPlatform(types.Ecosystem(strings.ToLower(platforms[0])))
	}
	return remote
}

func (v *hangarVersion) ToPackageRemoteForPlatform(platform types.Ecosystem) (
	types.ResolvedPackage,
	bool,
) {
	download, ok := v.downloadForPlatform(platform)
	if !ok {
		return types.ResolvedPackage{ID: types.VersionedPackageRef{PackageRef: types.PackageRef{Provider: types.ProviderHangar}}}, false
	}

	remote := types.ResolvedPackage{
		ID:       types.VersionedPackageRef{PackageRef: types.PackageRef{Provider: types.ProviderHangar}},
		FileURL:  download.URL(),
		Filename: download.FileInfo.Name,
	}

	if download.FileInfo.SHA256Hash != "" {
		remote.Hash = download.FileInfo.SHA256Hash
		remote.HashAlgorithm = "sha256"
	}

	return remote, true
}

// IsReleaseChannel reports whether the version was published on Hangar's
// Release channel. A version without channel data is treated as a release so
// older cached payloads keep resolving.
func (v *hangarVersion) IsReleaseChannel() bool {
	return v.Channel.Name == "" || strings.EqualFold(v.Channel.Name, "Release")
}

func (v *hangarVersion) PluginDependencyNames() []types.PackageName {
	depsForPlatform := v.DependenciesForPlatform(types.EcosystemUnspecified)
	if len(depsForPlatform) == 0 {
		return nil
	}

	deps := make([]types.PackageName, 0, len(depsForPlatform))
	for _, dep := range depsForPlatform {
		if dep.Name == "" || dep.ExternalURL != nil {
			continue
		}
		deps = append(deps, input.ToProjectName(dep.Name))
	}
	slices.Sort(deps)
	return deps
}

func (v *hangarVersion) DependenciesForPlatform(platform types.Ecosystem) []hangarPluginDependency {
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

func (v *hangarVersion) HasDownloadForPlatform(platform types.Ecosystem) bool {
	_, ok := v.downloadForPlatform(preferredDownloadPlatform(platform))
	if ok {
		return true
	}
	return len(v.Downloads) > 0 && platform == types.EcosystemUnspecified
}

func (v *hangarVersion) SupportsPlatform(platform types.Ecosystem) bool {
	if len(v.PlatformDependencies) == 0 {
		return false
	}

	preferredKey := strings.ToUpper(preferredDownloadPlatform(platform).String())
	if versions := v.PlatformDependencies[preferredKey]; len(versions) > 0 {
		return true
	}

	return platform == types.EcosystemUnspecified && len(v.PlatformDependencies) > 0
}

func (v *hangarVersion) downloadForPlatform(platform types.Ecosystem) (
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

func classifyHangarURL(name string) types.URLType {
	switch strings.ToLower(name) {
	case "issues":
		return types.URLIssues
	case "source":
		return types.URLSource
	case "wiki":
		return types.URLWiki
	case "support", "discord":
		return types.URLForum
	case "website", "homepage", "hangar":
		return types.URLHome
	default:
		return types.URLMisc
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
	return slices.Sorted(maps.Keys(m))
}
