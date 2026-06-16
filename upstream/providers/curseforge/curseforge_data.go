package curseforge

import (
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

// --- API response wrappers ---

// searchResponse wraps the CurseForge /v1/mods/search response.
type searchResponse struct {
	Data       []modResponse `json:"data"`
	Pagination pagination    `json:"pagination"`
}

func (s *searchResponse) ToSearchResults(source types.SourceId) upstream.SearchResponse {
	res := upstream.SearchResponse{
		Source: source,
		Items:  make([]upstream.RemotePackageName, 0, len(s.Data)),
	}
	for _, mod := range s.Data {
		res.Items = append(
			res.Items, upstream.RemotePackageName{
				RemoteName: input.ToProjectName(mod.Slug).String(),
				Source:     source,
			},
		)
	}
	return res
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
	Id                   int32          `json:"id"`
	GameId               int32          `json:"gameId"`
	Name                 string         `json:"name"`
	Slug                 string         `json:"slug"`
	Links                modLinks       `json:"links"`
	Summary              string         `json:"summary"`
	Status               int32          `json:"status"`
	DownloadCount        int64          `json:"downloadCount"`
	IsFeatured           bool           `json:"isFeatured"`
	PrimaryCategoryId    int32          `json:"primaryCategoryId"`
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

func (m *modResponse) ToProjectInformation() types.Metadata {
	return rawProjectInformation{mod: m}.ToProjectInformation()
}

func (r rawProjectInformation) ToProjectInformation() types.Metadata {
	m := r.mod
	info := types.Metadata{
		Title:                 m.Name,
		Brief:                 m.Summary,
		Description:           r.description,
		DescriptionIsMarkdown: upstream.LooksLikeMarkdown(r.description),
		Urls:                  make([]types.Url, 0),
		Authors:               make([]types.Person, 0, len(m.Authors)),
	}

	if m.Links.WebsiteUrl != "" {
		info.Urls = append(
			info.Urls, types.Url{
				Name: "Website",
				Type: types.UrlHome,
				Url:  m.Links.WebsiteUrl,
			},
		)
	}
	if m.Links.WikiUrl != "" {
		info.Urls = append(
			info.Urls, types.Url{
				Name: "Wiki",
				Type: types.UrlWiki,
				Url:  m.Links.WikiUrl,
			},
		)
	}
	if m.Links.IssuesUrl != "" {
		info.Urls = append(
			info.Urls, types.Url{
				Name: "Issues",
				Type: types.UrlIssues,
				Url:  m.Links.IssuesUrl,
			},
		)
	}
	if m.Links.SourceUrl != "" {
		info.Urls = append(
			info.Urls, types.Url{
				Name: "Source",
				Type: types.UrlSource,
				Url:  m.Links.SourceUrl,
			},
		)
	}

	for _, author := range m.Authors {
		info.Authors = append(
			info.Authors, types.Person{
				Name: author.Name,
				Url:  author.Url,
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
	Id   int32  `json:"id"`
	Name string `json:"name"`
	Url  string `json:"url"`
}

type modAsset struct {
	Id           int32  `json:"id"`
	ModId        int32  `json:"modId"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	ThumbnailUrl string `json:"thumbnailUrl"`
	Url          string `json:"url"`
}

// --- File-level structs ---

// filesResponse wraps /v1/mods/{modId}/files response.
type filesResponse struct {
	Data       []fileResponse `json:"data"`
	Pagination pagination     `json:"pagination"`
}

// fileResponse is the CurseForge File schema.
type fileResponse struct {
	Id            int32      `json:"id"`
	GameId        int32      `json:"gameId"`
	ModId         int32      `json:"modId"`
	IsAvailable   bool       `json:"isAvailable"`
	DisplayName   string     `json:"displayName"`
	FileName      string     `json:"fileName"`
	ReleaseType   int32      `json:"releaseType"` // 1=Release, 2=Beta, 3=Alpha
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

func (f *fileResponse) ToPackageRemote() types.PackageRemote {
	remote := types.PackageRemote{
		Source:   types.SourceCurseForge,
		Filename: f.FileName,
	}

	if f.DownloadUrl != nil {
		remote.FileUrl = *f.DownloadUrl
	}

	// Prefer SHA1 over MD5 (algo 1=sha1, 2=md5)
	for _, h := range f.Hashes {
		if h.Algo == 1 {
			remote.Hash = h.Value
			remote.HashAlgorithm = "sha1"
			break
		}
	}
	if remote.Hash == "" {
		for _, h := range f.Hashes {
			if h.Algo == 2 {
				remote.Hash = h.Value
				remote.HashAlgorithm = "md5"
				break
			}
		}
	}

	return remote
}

type fileHash struct {
	Value string `json:"value"`
	Algo  int32  `json:"algo"` // 1=Sha1, 2=Md5
}

type fileDependency struct {
	ModId        int32 `json:"modId"`
	RelationType int32 `json:"relationType"` // 3=Required, 2=Optional, etc.
}

type fileIndex struct {
	GameVersion       string `json:"gameVersion"`
	FileId            int32  `json:"fileId"`
	Filename          string `json:"filename"`
	ReleaseType       int32  `json:"releaseType"`
	GameVersionTypeId *int32 `json:"gameVersionTypeId"`
	ModLoader         *int32 `json:"modLoader"` // 0=Any, 1=Forge, 4=Fabric, 6=NeoForge
}

type pagination struct {
	Index       int32 `json:"index"`
	PageSize    int32 `json:"pageSize"`
	ResultCount int32 `json:"resultCount"`
	TotalCount  int32 `json:"totalCount"`
}
