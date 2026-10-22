package catalog

// Metadata contains informational data about a package. Archive readers and
// upstream providers both produce it. It carries content only. The source of
// the data is part of the result types, for example upstream.Info.
type Metadata struct {
	Title                 string   `json:"title"`
	Brief                 string   `json:"brief,omitempty"`
	Description           string   `json:"description,omitempty"`
	DescriptionURL        string   `json:"description_url,omitempty"`
	DescriptionIsMarkdown bool     `json:"description_is_markdown,omitempty"`
	Authors               []Person `json:"authors,omitempty"`
	URLs                  []URL    `json:"urls,omitempty"`
	License               string   `json:"license,omitempty"`
}

type Person struct {
	Name  string `json:"name"`
	Role  string `json:"role,omitempty"`
	URL   string `json:"url,omitempty"`
	Email string `json:"email,omitempty"`
}

type URL struct {
	Name string  `json:"name"`
	Type URLType `json:"type"`
	URL  string  `json:"url"`
}

func (p URLType) String() string {
	switch p {
	case URLFile:
		return "File"
	case URLHome:
		return "Homepage"
	case URLSource:
		return "Source"
	case URLWiki:
		return "Wiki"
	case URLForum:
		return "Forum"
	case URLIssues:
		return "Issues"
	case URLSponsor:
		return "Sponsor"
	case URLMisc:
		return "URL"
	default:
		return "Unknown"
	}
}

type URLType uint8

const (
	URLFile URLType = iota
	URLHome
	URLSource
	URLWiki
	URLForum
	URLIssues
	URLSponsor
	URLMisc
)
