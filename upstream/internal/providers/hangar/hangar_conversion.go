package hangar

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"net/url"
	"path"
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

// ToSearchResults preserves the owner-qualified identity, which is the
// coordinate an install can resolve without relying on a display name that
// two projects could share.
func (s *projectSearchResponse) ToSearchResults(source catalog.Provider) upstream.SearchResponse {
	res := upstream.SearchResponse{
		Provider: source,
		Items:    make([]upstream.SearchResult, 0, len(s.Result)),
	}

	for _, project := range s.Result {
		ref := project.ProjectRef()
		res.Items = append(
			res.Items, upstream.SearchResult{
				ProjectName: ref.LookupPath(),
				Provider:    source,
				Title:       project.Name,
				Description: project.Description,
				LastUpdated: hangarPublishedAt(project.LastUpdated),
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
			linkURL := safeHangarURL(link.URL)
			if linkURL == "" {
				continue
			}
			info.URLs = append(
				info.URLs, catalog.URL{
					Name: link.Name,
					Type: classifyHangarURL(link.Name),
					URL:  linkURL,
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

func hangarPackageRef() catalog.VersionedRef {
	return catalog.VersionedRef{
		Ref: catalog.Ref{Provider: catalog.ProviderHangar},
	}
}

func (v *hangarVersion) ToPackageRemote() upstream.ResolvedPackage {
	for _, key := range hangarPreferredPlatforms {
		if remote, ok := v.packageRemoteForPlatformKey(key); ok {
			return remote
		}
	}
	for _, key := range sortedMapKeys(v.Downloads) {
		if remote, ok := v.packageRemoteForPlatformKey(key); ok {
			return remote
		}
	}
	return upstream.ResolvedPackage{ID: hangarPackageRef()}
}

// ToPackageRemoteForPlatform resolves the artifact for exactly the requested
// platform. A request for one platform never receives another platform's
// file, because a Hangar version publishes platform-specific downloads.
func (v *hangarVersion) ToPackageRemoteForPlatform(platform catalog.Ecosystem) (
	upstream.ResolvedPackage,
	bool,
) {
	keys, supported := hangarPlatformKeysFor(platform)
	if !supported || len(keys) == 0 {
		return upstream.ResolvedPackage{ID: hangarPackageRef()}, false
	}
	for _, key := range keys {
		if remote, ok := v.packageRemoteForPlatformKey(key); ok {
			return remote, true
		}
	}
	return upstream.ResolvedPackage{ID: hangarPackageRef()}, false
}

func (v *hangarVersion) packageRemoteForPlatformKey(platformKey string) (
	upstream.ResolvedPackage,
	bool,
) {
	download, ok := v.downloadForPlatformKey(platformKey)
	if !ok {
		return upstream.ResolvedPackage{ID: hangarPackageRef()}, false
	}
	filename := download.metadataFilename()
	if filename == "" {
		return upstream.ResolvedPackage{ID: hangarPackageRef()}, false
	}

	remote := upstream.ResolvedPackage{
		ID:       hangarPackageRef(),
		FileURL:  download.URL(),
		Filename: filename,
	}
	if hash := download.SHA256(); hash != "" {
		remote.Hash = hash
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

// platformPluginDependencies returns the plugin dependencies Hangar declared
// for one platform. known is false only when the response carried no
// dependency object at all, which is different from an empty declaration.
func (v *hangarVersion) platformPluginDependencies(platformKey string) (
	[]hangarPluginDependency,
	bool,
) {
	if v.PluginDependencies == nil {
		return []hangarPluginDependency{}, false
	}
	if len(v.PluginDependencies) == 0 {
		return []hangarPluginDependency{}, true
	}
	for key, dependencies := range v.PluginDependencies {
		if !strings.EqualFold(key, platformKey) {
			continue
		}
		if dependencies == nil {
			return []hangarPluginDependency{}, true
		}
		return dependencies, true
	}
	// Another platform declared dependencies, but this platform's list is
	// unknown. Falling back would attribute another platform's dependencies.
	return []hangarPluginDependency{}, false
}

// platformDependenciesFor returns the Minecraft versions declared for one
// platform. ok is false when the response carried no entry for the platform,
// which leaves compatibility unverified rather than incompatible.
func (v *hangarVersion) platformDependenciesFor(platformKey string) (
	[]string,
	bool,
) {
	if len(v.PlatformDependencies) == 0 {
		return nil, false
	}
	for key, versions := range v.PlatformDependencies {
		if strings.EqualFold(key, platformKey) {
			return versions, true
		}
	}
	return nil, false
}

func (v *hangarVersion) PluginDependencyNames() []catalog.Name {
	var declared []hangarPluginDependency
	for _, key := range hangarPreferredPlatforms {
		if dependencies, known := v.platformPluginDependencies(key); known {
			declared = dependencies
			if len(declared) > 0 {
				break
			}
		}
	}
	if len(declared) == 0 {
		for _, key := range sortedMapKeys(v.PluginDependencies) {
			if dependencies := v.PluginDependencies[key]; len(dependencies) > 0 {
				declared = dependencies
				break
			}
		}
	}
	if len(declared) == 0 {
		return nil
	}

	deps := make([]catalog.Name, 0, len(declared))
	for _, dep := range declared {
		if dep.Name == "" || dep.ExternalURL != nil && *dep.ExternalURL != "" {
			continue
		}
		deps = append(deps, input.ToProjectName(dep.Name))
	}
	slices.Sort(deps)
	return deps
}

// safeHangarURL accepts only absolute HTTP(S) URLs. Remote metadata is
// untrusted input and must not become a local file path or another scheme.
func safeHangarURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
	default:
		return ""
	}
	if parsed.Host == "" {
		return ""
	}
	return parsed.String()
}

// hangarArtifactFilename accepts a single path segment only. Download
// metadata is the only filename source: a URL path can change independently
// of the uploaded artifact and must not rename what the lock pins.
func hangarArtifactFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return ""
	}
	if path.Base(name) != name || strings.ContainsAny(name, `/\`) {
		return ""
	}
	return name
}

func (d hangarDownload) metadataFilename() string {
	return hangarArtifactFilename(d.FileInfo.Name)
}

func (d hangarDownload) URL() string {
	if direct := safeHangarURL(d.DownloadURL); direct != "" {
		return direct
	}
	if d.ExternalURL == nil {
		return ""
	}
	return safeHangarURL(*d.ExternalURL)
}

func (d hangarDownload) SHA256() string {
	hash := strings.ToLower(strings.TrimSpace(d.FileInfo.SHA256Hash))
	if len(hash) != sha256.Size*2 {
		return ""
	}
	if _, err := hex.DecodeString(hash); err != nil {
		return ""
	}
	return hash
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
