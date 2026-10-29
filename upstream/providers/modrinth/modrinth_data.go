package modrinth

import (
	"time"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
)

// projectResponse
//
// API Example:
//   - https://api.modrinth.com/v2/project/P7dR8mSH
//     (Fabric API)
//   - https://api.modrinth.com/v2/project/1IjD5062
//     (Continuity)
type projectResponse struct {
	ClientSide       string    `json:"client_side"`
	ServerSide       string    `json:"server_side"`
	GameVersions     []string  `json:"game_versions"`
	ID               string    `json:"id"`
	Slug             string    `json:"slug"`
	ProjectType      string    `json:"project_type"`
	Team             string    `json:"team"`
	Organization     string    `json:"organization"`
	Title            string    `json:"title"`
	Description      string    `json:"description"`
	Body             string    `json:"body"`
	BodyUrl          string    `json:"body_url"`
	PublishedAt      time.Time `json:"published"`
	Updated          time.Time `json:"updated"`
	Approved         time.Time `json:"approved"`
	Queued           any       `json:"queued"`
	Status           string    `json:"status"`
	RequestedStatus  any       `json:"requested_status"`
	ModeratorMessage any       `json:"moderator_message"`
	License          struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		URL  any    `json:"url"`
	} `json:"license"`
	Downloads            int      `json:"downloads"`
	Followers            int      `json:"followers"`
	Categories           []string `json:"categories"`
	AdditionalCategories []string `json:"additional_categories"`
	Loaders              []string `json:"loaders"`
	Versions             []string `json:"versions"`
	IconUrl              string   `json:"icon_url"`
	IssuesUrl            string   `json:"issues_url"`
	SourceUrl            string   `json:"source_url"`
	WikiUrl              string   `json:"wiki_url"`
	DiscordUrl           string   `json:"discord_url"`
	DonationUrls         []struct {
		ID       string `json:"id"`
		Platform string `json:"platform"`
		URL      string `json:"url"`
	} `json:"donation_urls"`
	Gallery            []any  `json:"gallery"`
	Color              int    `json:"color"`
	ThreadId           string `json:"thread_id"`
	MonetizationStatus string `json:"monetization_status"`
}

func (p *projectResponse) ToProjectInformation() (info catalog.Metadata) {
	info = catalog.Metadata{
		Title:                 p.Title,
		Brief:                 p.Description,
		Description:           p.Body,
		DescriptionIsMarkdown: upstream.LooksLikeMarkdown(p.Body),
		License:               p.License.Name,
		URLs:                  make([]catalog.URL, 0),
	}

	if p.DiscordUrl != "" {
		info.URLs = append(
			info.URLs, catalog.URL{
				Name: "Discord",
				Type: catalog.URLForum,
				URL:  p.DiscordUrl,
			},
		)
	}

	if p.IssuesUrl != "" {
		info.URLs = append(
			info.URLs, catalog.URL{
				Name: "Issues",
				Type: catalog.URLSource,
				URL:  "",
			},
		)
	}

	if p.SourceUrl != "" {
		info.URLs = append(
			info.URLs, catalog.URL{
				Name: "Source",
				Type: catalog.URLSource,
				URL:  p.SourceUrl,
			},
		)
	}

	if p.WikiUrl != "" {
		info.URLs = append(
			info.URLs, catalog.URL{
				Name: "Wiki",
				Type: catalog.URLWiki,
				URL:  p.WikiUrl,
			},
		)
	}

	for _, donationUrl := range p.DonationUrls {
		info.URLs = append(
			info.URLs, catalog.URL{
				Name: donationUrl.Platform,
				Type: catalog.URLSponsor,
				URL:  donationUrl.URL,
			},
		)
	}

	return info
}

// searchResultResponse
//
// Docs
// https://docs.modrinth.com/api/operations/searchprojects/
//
// Example
// https://api.modrinth.com/v2/search?query=carpet&limit=100&index=relevance&facets=%5B%5B%22server_side:required%22,%22server_side:optional%22%5D%5D
type searchResultResponse struct {
	Hits      []*hit `json:"hits"`
	Offset    int    `json:"offset"`
	Limit     int    `json:"limit"`
	TotalHits int    `json:"total_hits"`
}

type hit struct {
	ProjectId         string    `json:"project_id"`
	ProjectType       string    `json:"project_type"`
	Slug              string    `json:"slug"`
	Author            string    `json:"author"`
	Title             string    `json:"title"`
	Description       string    `json:"description"`
	Categories        []string  `json:"categories"`
	DisplayCategories []string  `json:"display_categories"`
	Versions          []string  `json:"versions"`
	Downloads         int       `json:"downloads"`
	Follows           int       `json:"follows"`
	IconUrl           string    `json:"icon_url"`
	DateCreated       time.Time `json:"date_created"`
	DateModified      time.Time `json:"date_modified"`
	LatestVersion     string    `json:"latest_version"`
	License           string    `json:"license"`
	ClientSide        string    `json:"client_side"`
	ServerSide        string    `json:"server_side"`
	Gallery           []string  `json:"gallery"`
	FeaturedGallery   *string   `json:"featured_gallery"`
	Color             *int      `json:"color"`
}

// versionResponse
//
// Docs
// https://docs.modrinth.com/api/operations/getversion/
//
// Example
// https://api.modrinth.com/v2/version/F7LVluUL
type versionResponse struct {
	GameVersions    []string               `json:"game_versions"`
	Loaders         []string               `json:"loaders"`
	ID              string                 `json:"id"`
	ProjectId       string                 `json:"project_id"`
	AuthorId        string                 `json:"author_id"`
	Featured        bool                   `json:"featured"`
	Name            string                 `json:"name"`
	VersionNumber   string                 `json:"version_number"`
	Changelog       string                 `json:"changelog"`
	ChangelogUrl    string                 `json:"changelog_url"`
	DatePublished   time.Time              `json:"date_published"`
	Downloads       int                    `json:"downloads"`
	VersionType     string                 `json:"version_type"`
	Status          string                 `json:"status"`
	RequestedStatus any                    `json:"requested_status"`
	Files           []fileResponse         `json:"files"`
	Dependencies    []dependenciesResponse `json:"dependencies"`
}

func (v versionResponse) ToPackageRemote() upstream.ResolvedPackage {
	remote := upstream.ResolvedPackage{
		ID:       catalog.VersionedRef{Ref: catalog.Ref{Provider: catalog.ProviderModrinth}},
		FileURL:  v.Files[0].URL,
		Filename: v.Files[0].Filename,
	}

	if h := v.Files[0].Hashes; h.Sha512 != "" {
		remote.Hash = h.Sha512
		remote.HashAlgorithm = "sha512"
	} else if h.Sha1 != "" {
		remote.Hash = h.Sha1
		remote.HashAlgorithm = "sha1"
	}

	return remote
}

func resolvedPackageFromVersion(v versionResponse) upstream.ResolvedPackage {
	return v.ToPackageRemote()
}

type dependencyType string

const (
	required     dependencyType = "required"
	optional     dependencyType = "optional"
	incompatible dependencyType = "incompatible"
	embedded     dependencyType = "embedded"
)

type fileResponse struct {
	ID     string `json:"id"`
	Hashes struct {
		Sha1   string `json:"sha1"`
		Sha512 string `json:"sha512"`
	} `json:"hashes"`
	URL       string `json:"url"`
	Filename  string `json:"filename"`
	IsPrimary bool   `json:"primary"`
	Size      int    `json:"size"`
	FileType  string `json:"file_type"`
}

type dependenciesResponse struct {
	VersionId      string         `json:"version_id"`
	ProjectId      string         `json:"project_id"`
	FileName       string         `json:"file_name"`
	DependencyType dependencyType `json:"dependency_type"`
}

// memberResponse
//
// Docs
// https://docs.modrinth.com/api/operations/getprojectteammembers/
//
// Example
// https://api.modrinth.com/v2/project/carpet/members
type memberResponse struct {
	Role         string       `json:"role"`
	TeamId       string       `json:"team_id"`
	User         userResponse `json:"user"`
	Permissions  any          `json:"permissions"`
	Accepted     bool         `json:"accepted"`
	PayoutsSplit any          `json:"payouts_split"`
	Ordering     int          `json:"ordering"`
}

// userResponse
//
// # The url can either be an id or username
//
// Example
// https://modrinth.com/user/gnembon
type userResponse struct {
	ID                  string    `json:"id"`
	Username            string    `json:"username"`
	AvatarUrl           string    `json:"avatar_url"`
	Bio                 string    `json:"bio"`
	Created             time.Time `json:"created"`
	Role                string    `json:"role"`
	Badges              int       `json:"badges"`
	AuthProviders       string    `json:"auth_providers"`
	Email               string    `json:"email"`
	EmailVerified       bool      `json:"email_verified"`
	HasPassword         bool      `json:"has_password"`
	HasTotp             bool      `json:"has_totp"`
	PayoutData          string    `json:"payout_data"`
	StripeCustomerId    string    `json:"stripe_customer_id"`
	AllowFriendRequests bool      `json:"allow_friend_requests"`
	GithubId            string    `json:"github_id"`
}
