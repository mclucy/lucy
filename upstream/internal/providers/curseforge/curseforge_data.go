package curseforge

import (
	"time"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/upstream"
)

// --- API response wrappers ---

// searchResponse wraps the CurseForge /v1/mods/search response.
type searchResponse struct {
	Data       []modResponse `json:"data"`
	Pagination pagination    `json:"pagination"`
}

func (s *searchResponse) ToSearchResults(source catalog.Provider) upstream.SearchResponse {
	res := upstream.SearchResponse{
		Provider: source,
		Items:    make([]upstream.SearchResult, 0, len(s.Data)),
	}
	for _, mod := range s.Data {
		res.Items = append(
			res.Items, upstream.SearchResult{
				ProjectName: input.ToProjectName(mod.Slug).String(),
				Provider:    source,
				Title:       mod.Name,
				Description: mod.Summary,
				Downloads:   mod.DownloadCount,
				LastUpdated: parseCurseforgeDate(mod.DateModified),
			},
		)
	}
	return res
}

// parseCurseforgeDate parses CurseForge's RFC3339 timestamp.
// Returns zero time on failure or empty input.
func parseCurseforgeDate(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// modDataResponse wraps /v1/mods/{modId} response.
type modDataResponse struct {
	Data modResponse `json:"data"`
}

type stringDataResponse struct {
	Data string `json:"data"`
}

// modResponse is the CurseForge Mod schema.
type modResponse struct {
	ID                   int32          `json:"id"`
	GameId               int32          `json:"gameId"`
	Name                 string         `json:"name"`
	Slug                 string         `json:"slug"`
	Links                modLinks       `json:"links"`
	Summary              string         `json:"summary"`
	Status               int32          `json:"status"`
	DownloadCount        int64          `json:"downloadCount"`
	IsFeatured           bool           `json:"isFeatured"`
	CategoryID           int32          `json:"primaryCategoryId"`
	ClassId              *int32         `json:"classId"`
	Authors              []modAuthor    `json:"authors"`
	Logo                 *modAsset      `json:"logo"`
	MainFileId           int32          `json:"mainFileId"`
	LatestFiles          []fileResponse `json:"latestFiles"`
	LatestFilesIndexes   []fileIndex    `json:"latestFilesIndexes"`
	DateCreated          string         `json:"dateCreated"`
	DateModified         string         `json:"dateModified"`
	DateReleased         string         `json:"dateReleased"`
	AllowModDistribution *bool          `json:"allowModDistribution"`
	GamePopularityRank   int32          `json:"gamePopularityRank"`
	IsAvailable          bool           `json:"isAvailable"`
	ThumbsUpCount        int32          `json:"thumbsUpCount"`
}

type rawProjectInformation struct {
	mod         *modResponse
	description string
}

func (m *modResponse) ToProjectInformation() catalog.Metadata {
	return rawProjectInformation{mod: m}.ToProjectInformation()
}

func (r rawProjectInformation) ToProjectInformation() catalog.Metadata {
	m := r.mod
	info := catalog.Metadata{
		Title:                 m.Name,
		Brief:                 m.Summary,
		Description:           r.description,
		DescriptionIsMarkdown: upstream.LooksLikeMarkdown(r.description),
		URLs:                  make([]catalog.URL, 0),
		Authors:               make([]catalog.Person, 0, len(m.Authors)),
	}

	if m.Links.WebsiteUrl != "" {
		info.URLs = append(
			info.URLs, catalog.URL{
				Name: "Website",
				Type: catalog.URLHome,
				URL:  m.Links.WebsiteUrl,
			},
		)
	}
	if m.Links.WikiUrl != "" {
		info.URLs = append(
			info.URLs, catalog.URL{
				Name: "Wiki",
				Type: catalog.URLWiki,
				URL:  m.Links.WikiUrl,
			},
		)
	}
	if m.Links.IssuesUrl != "" {
		info.URLs = append(
			info.URLs, catalog.URL{
				Name: "Issues",
				Type: catalog.URLIssues,
				URL:  m.Links.IssuesUrl,
			},
		)
	}
	if m.Links.SourceUrl != "" {
		info.URLs = append(
			info.URLs, catalog.URL{
				Name: "Source",
				Type: catalog.URLSource,
				URL:  m.Links.SourceUrl,
			},
		)
	}

	for _, author := range m.Authors {
		info.Authors = append(
			info.Authors, catalog.Person{
				Name: author.Name,
				URL:  author.URL,
			},
		)
	}

	return info
}

type modLinks struct {
	WebsiteUrl string `json:"websiteUrl"`
	WikiUrl    string `json:"wikiUrl"`
	IssuesUrl  string `json:"issuesUrl"`
	SourceUrl  string `json:"sourceUrl"`
}

type modAuthor struct {
	ID   int32  `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type modAsset struct {
	ID           int32  `json:"id"`
	ModId        int32  `json:"modId"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	ThumbnailUrl string `json:"thumbnailUrl"`
	URL          string `json:"url"`
}

// --- File-level structs ---

// filesResponse wraps /v1/mods/{modId}/files response.
type filesResponse struct {
	Data       []fileResponse `json:"data"`
	Pagination pagination     `json:"pagination"`
}

// fileResponse is the CurseForge File schema.
type fileResponse struct {
	ID            int32      `json:"id"`
	GameId        int32      `json:"gameId"`
	ModId         int32      `json:"modId"`
	IsAvailable   bool       `json:"isAvailable"`
	DisplayName   string     `json:"displayName"`
	FileName      string     `json:"fileName"`
	ChannelCode   int32      `json:"releaseType"` // 1=Release, 2=Beta, 3=Alpha
	FileStatus    int32      `json:"fileStatus"`
	Hashes        []fileHash `json:"hashes"`
	FileDate      string     `json:"fileDate"`
	FileLength    int64      `json:"fileLength"`
	DownloadCount int64      `json:"downloadCount"`
	// Docs: https://docs.curseforge.com/rest-api/#get-mod-files
	DownloadUrl      *string          `json:"downloadUrl"` // CAN BE NULL
	GameVersions     []string         `json:"gameVersions"`
	Dependencies     []fileDependency `json:"dependencies"`
	IsServerPack     *bool            `json:"isServerPack"`
	ServerPackFileId *int32           `json:"serverPackFileId"`
}

func (f *fileResponse) ToPackageRemote() upstream.ResolvedPackage {
	remote := upstream.ResolvedPackage{
		ID:       catalog.VersionedRef{Ref: catalog.Ref{Provider: catalog.ProviderCurseForge}},
		Filename: f.FileName,
	}

	if f.DownloadUrl != nil {
		remote.FileURL = *f.DownloadUrl
	}

	// Prefer SHA1 over MD5 (algo 1=sha1, 2=md5)
	for _, h := range f.Hashes {
		if h.Algo == 1 {
			remote.Hash = h.Digest
			remote.HashAlgorithm = "sha1"
			break
		}
	}
	if remote.Hash == "" {
		for _, h := range f.Hashes {
			if h.Algo == 2 {
				remote.Hash = h.Digest
				remote.HashAlgorithm = "md5"
				break
			}
		}
	}

	return remote
}

type fileHash struct {
	Digest string `json:"value"`
	Algo   int32  `json:"algo"` // 1=Sha1, 2=Md5
}

type fileDependency struct {
	ModId        int32 `json:"modId"`
	RelationType int32 `json:"relationType"` // 3=Required, 2=Optional, etc.
}

type fileIndex struct {
	GameVersion       string `json:"gameVersion"`
	FileId            int32  `json:"fileId"`
	Filename          string `json:"filename"`
	ChannelCode       int32  `json:"releaseType"`
	GameVersionTypeId *int32 `json:"gameVersionTypeId"`
	ModLoader         *int32 `json:"modLoader"` // 0=Any, 1=Forge, 4=Fabric, 6=NeoForge
}

type pagination struct {
	Index       int32 `json:"index"`
	PageSize    int32 `json:"pageSize"`
	ResultCount int32 `json:"resultCount"`
	TotalCount  int32 `json:"totalCount"`
}
