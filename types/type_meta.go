package types

// Metadata contains informational data about a package. Archive readers and
// upstream providers both produce it. It carries content only. The source of
// the data is part of the result types, for example upstream.Info.
type Metadata struct {
	Title                 string
	Brief                 string // short
	Description           string // prose or Markdown
	DescriptionUrl        string // when full description is not displayed
	DescriptionIsMarkdown bool
	Authors               []Person
	Urls                  []Url
	License               string
}

type Person struct {
	Name  string
	Role  string
	Url   string
	Email string
}

type Url struct {
	Name string
	Type UrlType
	Url  string
}

func (p UrlType) String() string {
	switch p {
	case UrlFile:
		return "File"
	case UrlHome:
		return "Homepage"
	case UrlSource:
		return "Source"
	case UrlWiki:
		return "Wiki"
	case UrlMisc:
		return "URL"
	default:
		return "Unknown"
	}
}

type UrlType uint8

const (
	UrlFile UrlType = iota
	UrlHome
	UrlSource
	UrlWiki
	UrlForum
	UrlIssues
	UrlSponsor
	UrlMisc
)
