package spiget

import (
	"encoding/base64"
	"net/url"
	"path"
	"strconv"
	"strings"
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

// ToSearchResults exposes Spiget's numeric resource ID as the project
// identity. SpigotMC publishes several resources under one display name, so a
// normalized name is not stable or installable; the ID is, and the display name
// travels as the result title.
func (s searchResponse) ToSearchResults(source catalog.Provider) upstream.SearchResponse {
	results := upstream.SearchResponse{
		Provider: source,
		Items:    make([]upstream.SearchResult, 0, len(s)),
	}

	for _, resource := range s {
		if resource.ID <= 0 || resource.Name == "" {
			continue
		}
		results.Items = append(
			results.Items, upstream.SearchResult{
				ProjectName: strconv.FormatInt(resource.ID, 10),
				Provider:    source,
				Title:       resource.Name,
				Description: resource.Tag,
				Downloads:   resource.Downloads,
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
	build := upstream.Build{
		Version:   strings.TrimSpace(v.Name),
		Published: releaseTime(v.ReleaseDate),
	}
	if build.Version == "" && v.ID > 0 {
		build.Version = strconv.FormatInt(v.ID, 10)
	}
	return build
}

// spigetVersion reports the human version label, falling back to the numeric
// version ID when the publisher left the label empty. The ID is always kept
// separately as the release and file identity.
func spigetVersion(name string, id int64) versionpkg.Version {
	if name = strings.TrimSpace(name); name != "" {
		return versionpkg.Version(name)
	}
	if id > 0 {
		return versionpkg.Version(strconv.FormatInt(id, 10))
	}
	return versionpkg.Unknown
}

func (r resolvedVersion) LucyVersion() versionpkg.Version {
	return spigetVersion(r.VersionName, r.VersionID)
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
	if r.ResourceID == 0 {
		return ""
	}
	return resourceDownloadURL(r.ResourceID)
}

func (r resolvedVersion) filename() string {
	return artifactFilename(
		r.ResourceID,
		r.ProjectName,
		r.VersionName,
		r.VersionID,
		r.FileType,
		r.downloadURL(),
	)
}

// artifactFilename names the local file for one published version. Spiget's API
// reports the archive type but never the uploaded file name, so a Spiget-hosted
// version gets a name derived from provider fields. An external download URL
// names the file itself, so its last path element is used unchanged.
func artifactFilename(
	resourceID int64,
	projectName, versionName string,
	versionID int64,
	fileType, downloadURL string,
) string {
	if name := archiveFilename(downloadURL); name != "" {
		return name
	}
	base := normalizedProjectName(projectName).String()
	if base == "" {
		base = strconv.FormatInt(resourceID, 10)
	}
	label := sanitizeFilenamePart(versionName)
	if label == "" && versionID > 0 {
		label = strconv.FormatInt(versionID, 10)
	}
	if label != "" {
		base += "-" + label
	}
	if ext := normalizedFileExtension(fileType); ext != "" {
		return base + ext
	}
	return base
}

// archiveFilename returns the file name of a direct plugin-archive URL, or ""
// when the URL does not address one. Spiget's external URLs sometimes point at
// a release page rather than an archive; installing such a page would not
// produce a plugin.
func archiveFilename(rawURL string) string {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	name := path.Base(parsed.Path)
	if name == "" || name == "." || name == "/" {
		return ""
	}
	if !strings.EqualFold(path.Ext(name), ".jar") {
		return ""
	}
	if strings.ContainsAny(name, "/\\") {
		return ""
	}
	return name
}

// sanitizeFilenamePart keeps a version label safe for a file name without
// dropping its readable form.
func sanitizeFilenamePart(part string) string {
	part = strings.TrimSpace(part)
	var b strings.Builder
	b.Grow(len(part))
	lastReplaced := false
	for _, r := range part {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' ||
			r == '_' || r == '-':
			b.WriteRune(r)
			lastReplaced = false
		case !lastReplaced:
			b.WriteByte('-')
			lastReplaced = true
		}
	}
	return strings.Trim(b.String(), "-.")
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
