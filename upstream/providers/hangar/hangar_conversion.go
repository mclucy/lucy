package hangar

import (
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
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

func (r hangarProjectRef) CanonicalName() catalog.Name {
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

func (s *projectSearchResponse) ToSearchResults(source catalog.Provider) upstream.SearchResponse {
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

func (p *hangarProject) ToProjectInformation() catalog.Metadata {
	info := catalog.Metadata{
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
		Authors: make([]catalog.Person, 0, len(p.MemberNames)),
		URLs:    make([]catalog.URL, 0, len(p.Settings.Links)+1),
	}

	if projectURL := p.ProjectRef().ProjectURL(); projectURL != "" {
		info.URLs = append(
			info.URLs, catalog.URL{
				Name: "Hangar",
				Type: catalog.URLHome,
				URL:  projectURL,
			},
		)
	}

	for _, memberName := range p.MemberNames {
		info.Authors = append(info.Authors, catalog.Person{Name: memberName})
	}

	for _, section := range p.Settings.Links {
		for _, link := range section.Links {
			if link.URL == "" {
				continue
			}
			info.URLs = append(
				info.URLs, catalog.URL{
					Name: link.Name,
					Type: classifyHangarURL(link.Name),
					URL:  link.URL,
				},
			)
		}
	}

	return info
}

// ToBuilds maps one Hangar version to one build entry per declared platform.
// Game versions stay attached to the platform that declared them.
func (v *hangarVersion) ToBuilds() []upstream.Build {
	channel := strings.ToLower(strings.TrimSpace(v.Channel.Name))
	published := hangarPublishedAt(v.CreatedAt)

	platforms := make(map[string]struct{})
	for platform := range v.PlatformDependencies {
		platforms[strings.ToLower(platform)] = struct{}{}
	}
	for platform := range v.Downloads {
		platforms[strings.ToLower(platform)] = struct{}{}
	}

	if len(platforms) == 0 {
		return []upstream.Build{{
			Version:   v.Name,
			Channel:   channel,
			Published: published,
		}}
	}

	builds := make([]upstream.Build, 0, len(platforms))
	for _, platform := range sortedMapKeys(platforms) {
		builds = append(
			builds, upstream.Build{
				Version:      v.Name,
				GameVersions: v.platformGameVersions(platform),
				Loaders:      []string{platform},
				Channel:      channel,
				Published:    published,
			},
		)
	}
	return builds
}

func (v *hangarVersion) platformGameVersions(platform string) []string {
	for key, versions := range v.PlatformDependencies {
		if strings.EqualFold(key, platform) {
			return versions
		}
	}
	return nil
}

func hangarPublishedAt(raw string) time.Time {
	published, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return time.Time{}
	}
	return published
}

func (v *hangarVersion) ToPackageRemote() upstream.ResolvedPackage {
	remote, _ := v.ToPackageRemoteForPlatform(preferredDownloadPlatform(catalog.EcosystemUnspecified))
	if remote.FileURL == "" {
		platforms := sortedMapKeys(v.Downloads)
		if len(platforms) == 0 {
			return upstream.ResolvedPackage{ID: catalog.VersionedRef{Ref: catalog.Ref{Provider: catalog.ProviderHangar}}}
		}

		remote, _ = v.ToPackageRemoteForPlatform(catalog.Ecosystem(strings.ToLower(platforms[0])))
	}
	return remote
}

func (v *hangarVersion) ToPackageRemoteForPlatform(platform catalog.Ecosystem) (
	upstream.ResolvedPackage,
	bool,
) {
	download, ok := v.downloadForPlatform(platform)
	if !ok {
		return upstream.ResolvedPackage{ID: catalog.VersionedRef{Ref: catalog.Ref{Provider: catalog.ProviderHangar}}}, false
	}

	remote := upstream.ResolvedPackage{
		ID:       catalog.VersionedRef{Ref: catalog.Ref{Provider: catalog.ProviderHangar}},
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

func (v *hangarVersion) PluginDependencyNames() []catalog.Name {
	depsForPlatform := v.DependenciesForPlatform(catalog.EcosystemUnspecified)
	if len(depsForPlatform) == 0 {
		return nil
	}

	deps := make([]catalog.Name, 0, len(depsForPlatform))
	for _, dep := range depsForPlatform {
		if dep.Name == "" || dep.ExternalURL != nil {
			continue
		}
		deps = append(deps, input.ToProjectName(dep.Name))
	}
	slices.Sort(deps)
	return deps
}

func (v *hangarVersion) DependenciesForPlatform(platform catalog.Ecosystem) []hangarPluginDependency {
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

func (v *hangarVersion) HasDownloadForPlatform(platform catalog.Ecosystem) bool {
	_, ok := v.downloadForPlatform(preferredDownloadPlatform(platform))
	if ok {
		return true
	}
	return len(v.Downloads) > 0 && platform == catalog.EcosystemUnspecified
}

func (v *hangarVersion) SupportsPlatform(platform catalog.Ecosystem) bool {
	if len(v.PlatformDependencies) == 0 {
		return false
	}

	preferredKey := strings.ToUpper(preferredDownloadPlatform(platform).String())
	if versions := v.PlatformDependencies[preferredKey]; len(versions) > 0 {
		return true
	}

	return platform == catalog.EcosystemUnspecified && len(v.PlatformDependencies) > 0
}

func (v *hangarVersion) downloadForPlatform(platform catalog.Ecosystem) (
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

func classifyHangarURL(name string) catalog.URLType {
	switch strings.ToLower(name) {
	case "issues":
		return catalog.URLIssues
	case "source":
		return catalog.URLSource
	case "wiki":
		return catalog.URLWiki
	case "support", "discord":
		return catalog.URLForum
	case "website", "homepage", "hangar":
		return catalog.URLHome
	default:
		return catalog.URLMisc
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
