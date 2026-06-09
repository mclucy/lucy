package spiget

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode"

	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

const (
	spigetAPIBaseURL     = "https://api.spiget.org/v2"
	spigotWebsiteBaseURL = "https://www.spigotmc.org/"
)

type decodedHTML struct {
	Value string
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

func (s searchResponse) ToSearchResults(source types.SourceId) upstream.SearchResponse {
	results := upstream.SearchResponse{
		Source: source,
		Items:  make([]upstream.RemotePackageName, 0, len(s)),
	}

	for _, resource := range s {
		if resource.Name == "" {
			continue
		}
		results.Items = append(results.Items, upstream.RemotePackageName{
			RemoteName: normalizedProjectName(resource.Name).String(),
			Source:     source,
		})
	}

	return results
}

func (r resourceResponse) ToProjectInformation() types.Metadata {
	description := combineHTMLSections(
		decodeBase64HTML(r.Description),
		decodeBase64HTML(r.Documentation),
	)

	info := types.Metadata{
		Title:                 r.Name,
		Brief:                 r.Tag,
		Description:           description,
		DescriptionIsMarkdown: false,
		Urls:                  make([]types.Url, 0, len(r.Links)+2),
	}

	appendLinkURLs(&info, r.Links)
	if r.SourceCodeLink != "" {
		info.Urls = append(
			info.Urls,
			types.Url{
				Name: "Source", Type: types.UrlSource, Url: r.SourceCodeLink,
			},
		)
	}
	if r.DonationLink != "" {
		info.Urls = append(
			info.Urls,
			types.Url{
				Name: "Donate", Type: types.UrlSponsor, Url: r.DonationLink,
			},
		)
	}

	return info
}

func (r resourceResponse) ToProjectSupport() types.PlatformSupport {
	support := types.PlatformSupport{
		MinecraftVersions: make([]types.BareVersion, 0, len(r.TestedVersions)),
		Platforms:         make([]types.PlatformId, 0),
		Authentic:         false,
	}

	for _, version := range r.TestedVersions {
		if version == "" {
			continue
		}
		support.MinecraftVersions = append(
			support.MinecraftVersions,
			types.BareVersion(version),
		)
	}

	return support
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

func (r resolvedVersion) LucyVersion() types.BareVersion {
	if r.VersionName != "" {
		return types.BareVersion(r.VersionName)
	}
	if r.VersionID != 0 {
		return types.BareVersion(strconv.FormatInt(r.VersionID, 10))
	}
	return types.VersionUnknown
}

func (r resolvedVersion) Matches(version types.BareVersion) bool {
	requested := strings.TrimSpace(version.String())
	if requested == "" || requested == types.VersionAny.String() {
		return false
	}
	if requested == string(r.LucyVersion()) {
		return true
	}
	return r.VersionID != 0 && requested == strconv.FormatInt(r.VersionID, 10)
}

func (r resolvedVersion) ToPackageRemote() types.PackageRemote {
	return types.PackageRemote{
		Source:   types.SourceSpiget,
		FileUrl:  r.downloadURL(),
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
	return decodedHTML{Value: string(decoded), Valid: true}
}

func combineHTMLSections(description, documentation decodedHTML) string {
	switch {
	case description.Valid && documentation.Valid:
		return description.Value + "\n\n<hr />\n\n<h2>Documentation</h2>\n" + documentation.Value
	case description.Valid:
		return description.Value
	case documentation.Valid:
		return documentation.Value
	default:
		return ""
	}
}

func appendLinkURLs(info *types.Metadata, links map[string]string) {
	for key, rawValue := range links {
		value := normalizeSpigetURL(rawValue)
		if value == "" {
			continue
		}
		name, kind := classifySpigetLink(key)
		info.Urls = append(
			info.Urls,
			types.Url{Name: name, Type: kind, Url: value},
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

func classifySpigetLink(key string) (string, types.UrlType) {
	switch key {
	case "additionalInformation":
		return "Additional information", types.UrlHome
	case "alternativeSupport":
		return "Support", types.UrlForum
	case "discussion":
		return "Discussion", types.UrlForum
	}

	decoded := decodeBase64HTML(key)
	if decoded.Valid {
		if _, err := url.ParseRequestURI(decoded.Value); err == nil {
			return "Link", types.UrlMisc
		}
		return prettifyLinkName(decoded.Value), types.UrlMisc
	}

	return prettifyLinkName(key), types.UrlMisc
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

func normalizedProjectName(name string) types.BarePackageName {
	name = syntax.ToProjectName(name).String()
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

	return types.BarePackageName(strings.Trim(b.String(), "-"))
}
