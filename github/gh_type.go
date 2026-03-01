package github

// Make this independent if some other package needs to access GitHub API

// GhItem is the GitHub API representation of a file or directory item
type GhItem struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Sha         string `json:"sha"`
	Size        int    `json:"size"`
	Url         string `json:"url"`
	HtmlUrl     string `json:"html_url"`
	GitUrl      string `json:"git_url"`
	DownloadUrl string `json:"download_url"`
	Type        string `json:"type"` // "file", "dir", etc.
	Links       struct {
		Self string `json:"self"`
		Git  string `json:"git"`
		Html string `json:"html"`
	} `json:"_links"`
}

// GhApiMessage is the message from GitHub API when something goes wrong
type GhApiMessage struct {
	Message          string `json:"message"`
	DocumentationUrl string `json:"documentation_url"`
	Status           string `json:"status"`
}
