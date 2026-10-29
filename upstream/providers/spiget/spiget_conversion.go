package spiget

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

const (
	spigetAPIBaseURL     = "https://api.spiget.org/v2"
	spigotWebsiteBaseURL = "https://www.spigotmc.org/"
)

type decodedHTML struct {
	Text  string
	Valid bool
}

// resolvedVersion keeps enough provider-local identity to resolve exact Spiget
// downloads while still exposing Lucy-friendly human version names.
type resolvedVersion struct {
	ResourceID  int64
	VersionID   int64
	VersionName string
	ProjectName string
	FileType    string
	External    bool
	ExternalURL string
	UUID        string
}

func (s searchResponse) ToSearchResults(source catalog.Provider) upstream.SearchResponse {
	results := upstream.SearchResponse{
		Provider: source,
		Items:    make([]upstream.SearchResult, 0, len(s)),
	}

	for _, resource := range s {
		if resource.Name == "" {
			continue
		}
		results.Items = append(
			results.Items, upstream.SearchResult{
				ProjectName: normalizedProjectName(resource.Name).String(),
				Provider:    source,
			},
		)
	}

	return results
}

func (r resourceResponse) ToProjectInformation() catalog.Metadata {
	description := combineHTMLSections(
		decodeBase64HTML(r.Description),
		decodeBase64HTML(r.Documentation),
	)

	info := catalog.Metadata{
		Title:                 r.Name,
		Brief:                 r.Tag,
		Description:           description,
		DescriptionIsMarkdown: false,
		URLs:                  make([]catalog.URL, 0, len(r.Links)+2),
	}

	appendLinkURLs(&info, r.Links)
	if r.SourceCodeLink != "" {
		info.URLs = append(
			info.URLs,
			catalog.URL{
				Name: "Source", Type: catalog.URLSource, URL: r.SourceCodeLink,
			},
		)
	}
	if r.DonationLink != "" {
		info.URLs = append(
			info.URLs,
			catalog.URL{
				Name: "Donate", Type: catalog.URLSponsor, URL: r.DonationLink,
			},
		)
	}

	return info
}

// NewResolvedVersion preserves both Lucy-facing human versions and Spiget's
// numeric resource/version identifiers for later exact download resolution.
func NewResolvedVersion(
	resource resourceResponse,
	version versionResponse,
) resolvedVersion {
	return resolvedVersion{
		ResourceID:  resource.ID,
		VersionID:   version.ID,
		VersionName: version.Name,
		ProjectName: normalizedProjectName(resource.Name).String(),
		FileType:    resource.File.Type,
		External:    resource.External,
		ExternalURL: resource.File.ExternalURL,
		UUID:        version.UUID,
	}
}

// ToBuild maps one Spiget resource version to a build. Spiget version records
// carry no game versions, loaders, or channel; resource-level tested versions
// are not build-specific and stay out of the build.
func (v versionResponse) ToBuild() upstream.Build {
	build := upstream.Build{Version: v.Name}
	if build.Version == "" && v.ID != 0 {
		build.Version = strconv.FormatInt(v.ID, 10)
	}
	if v.ReleaseDate > 0 {
		build.Published = time.Unix(v.ReleaseDate, 0).UTC()
	}
	return build
}

func (r resolvedVersion) LucyVersion() versionpkg.Version {
	if r.VersionName != "" {
		return versionpkg.Version(r.VersionName)
	}
	if r.VersionID != 0 {
		return versionpkg.Version(strconv.FormatInt(r.VersionID, 10))
	}
	return versionpkg.Unknown
}

func (r resolvedVersion) Matches(version versionpkg.Version) bool {
	requested := strings.TrimSpace(version.String())
	if requested == "" || requested == versionpkg.Any.String() {
		return false
	}
	if requested == string(r.LucyVersion()) {
		return true
	}
	return r.VersionID != 0 && requested == strconv.FormatInt(r.VersionID, 10)
}

func (r resolvedVersion) ToPackageRemote() upstream.ResolvedPackage {
	return upstream.ResolvedPackage{
		ID:       catalog.VersionedRef{Ref: catalog.Ref{Provider: catalog.ProviderSpiget}},
		FileURL:  r.downloadURL(),
		Filename: r.filename(),
	}
}

func (r resolvedVersion) downloadURL() string {
	if r.External && r.ExternalURL != "" {
		return r.ExternalURL
	}
	if r.ResourceID == 0 || r.VersionID == 0 {
		return ""
	}
	return fmt.Sprintf(
		"%s/resources/%d/versions/%d/download",
		spigetAPIBaseURL,
		r.ResourceID,
		r.VersionID,
	)
}

func (r resolvedVersion) filename() string {
	base := strings.TrimSpace(r.ProjectName)
	if base == "" {
		base = strconv.FormatInt(r.ResourceID, 10)
	}
	version := strings.TrimSpace(r.VersionName)
	if version == "" && r.VersionID != 0 {
		version = strconv.FormatInt(r.VersionID, 10)
	}
	if version != "" {
		base += "-" + version
	}
	if ext := normalizedFileExtension(r.FileType); ext != "" {
		return base + ext
	}
	return base
}

func normalizedFileExtension(fileType string) string {
	trimmed := strings.TrimSpace(fileType)
	if trimmed == "" || strings.EqualFold(trimmed, "external") {
		return ""
	}
	if strings.HasPrefix(trimmed, ".") {
		return trimmed
	}
	return "." + trimmed
}

func decodeBase64HTML(encoded string) decodedHTML {
	encoded = strings.TrimSpace(encoded)
	if encoded == "" {
		return decodedHTML{}
	}
	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return decodedHTML{}
	}
	return decodedHTML{Text: string(decoded), Valid: true}
}

func combineHTMLSections(description, documentation decodedHTML) string {
	switch {
	case description.Valid && documentation.Valid:
		return description.Text + "\n\n<hr />\n\n<h2>Documentation</h2>\n" + documentation.Text
	case description.Valid:
		return description.Text
	case documentation.Valid:
		return documentation.Text
	default:
		return ""
	}
}

func appendLinkURLs(info *catalog.Metadata, links map[string]string) {
	for key, rawValue := range links {
		value := normalizeSpigetURL(rawValue)
		if value == "" {
			continue
		}
		name, kind := classifySpigetLink(key)
		info.URLs = append(
			info.URLs,
			catalog.URL{Name: name, Type: kind, URL: value},
		)
	}
}

func normalizeSpigetURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, "/") {
		return spigotWebsiteBaseURL[:len(spigotWebsiteBaseURL)-1] + raw
	}
	if strings.HasPrefix(raw, "threads/") || strings.HasPrefix(
		raw,
		"resources/",
	) || strings.HasPrefix(raw, "members/") {
		return spigotWebsiteBaseURL + raw
	}
	return raw
}

func classifySpigetLink(key string) (string, catalog.URLType) {
	switch key {
	case "additionalInformation":
		return "Additional information", catalog.URLHome
	case "alternativeSupport":
		return "Support", catalog.URLForum
	case "discussion":
		return "Discussion", catalog.URLForum
	}

	decoded := decodeBase64HTML(key)
	if decoded.Valid {
		if _, err := url.ParseRequestURI(decoded.Text); err == nil {
			return "Link", catalog.URLMisc
		}
		return prettifyLinkName(decoded.Text), catalog.URLMisc
	}

	return prettifyLinkName(key), catalog.URLMisc
}

func prettifyLinkName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Link"
	}
	replacer := strings.NewReplacer("-", " ", "_", " ")
	name = replacer.Replace(name)
	return strings.ToUpper(name[:1]) + name[1:]
}

func normalizedProjectName(name string) catalog.Name {
	name = input.ToProjectName(name).String()
	var b strings.Builder
	b.Grow(len(name))
	lastHyphen := false

	for _, r := range name {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(unicode.ToLower(r))
			lastHyphen = false
		case !lastHyphen:
			b.WriteByte('-')
			lastHyphen = true
		}
	}

	return catalog.Name(strings.Trim(b.String(), "-"))
}
