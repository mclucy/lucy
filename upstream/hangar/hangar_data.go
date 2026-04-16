package hangar

type hangarPagination struct {
	Count  int `json:"count"`
	Limit  int `json:"limit"`
	Offset int `json:"offset"`
}

type projectSearchResponse struct {
	Pagination hangarPagination `json:"pagination"`
	Result     []hangarProject  `json:"result"`
}

type hangarProject struct {
	CreatedAt          string                   `json:"createdAt"`
	ID                 int64                    `json:"id"`
	Name               string                   `json:"name"`
	Namespace          hangarProjectNamespace   `json:"namespace"`
	Category           string                   `json:"category"`
	Description        string                   `json:"description"`
	LastUpdated        string                   `json:"lastUpdated"`
	Visibility         string                   `json:"visibility"`
	Settings           hangarProjectSettings    `json:"settings"`
	SupportedPlatforms hangarPlatformVersionMap `json:"supportedPlatforms"`
	MainPageContent    string                   `json:"mainPageContent"`
	MemberNames        []string                 `json:"memberNames"`
	AvatarURL          string                   `json:"avatarUrl"`
}

type hangarProjectNamespace struct {
	Owner string `json:"owner"`
	Slug  string `json:"slug"`
}

type hangarProjectSettings struct {
	Links    []hangarLinkSection  `json:"links"`
	Tags     []string             `json:"tags"`
	License  hangarProjectLicense `json:"license"`
	Keywords []string             `json:"keywords"`
	Sponsors *string              `json:"sponsors"`
	Donation hangarDonation       `json:"donation"`
}

type hangarProjectLicense struct {
	Name string  `json:"name"`
	URL  *string `json:"url"`
	Type string  `json:"type"`
}

type hangarDonation struct {
	Subject string `json:"subject"`
	Enable  bool   `json:"enable"`
}

type hangarLinkSection struct {
	ID    int          `json:"id"`
	Type  string       `json:"type"`
	Title *string      `json:"title"`
	Links []hangarLink `json:"links"`
}

type hangarLink struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type hangarPlatformVersionMap map[string][]string

type HangarVersionListResponse struct {
	Pagination hangarPagination `json:"pagination"`
	Result     []hangarVersion  `json:"result"`
}

type hangarVersion struct {
	CreatedAt                     string                            `json:"createdAt"`
	ID                            int64                             `json:"id"`
	ProjectID                     int64                             `json:"projectId"`
	Name                          string                            `json:"name"`
	Description                   string                            `json:"description"`
	Author                        string                            `json:"author"`
	Downloads                     map[string]hangarDownload         `json:"downloads"`
	PluginDependencies            map[string]hangarPluginDependency `json:"pluginDependencies"`
	PlatformDependencies          hangarPlatformVersionMap          `json:"platformDependencies"`
	PlatformDependenciesFormatted hangarPlatformVersionMap          `json:"platformDependenciesFormatted"`
}

type hangarDownload struct {
	FileInfo    hangarFileInfo `json:"fileInfo"`
	ExternalURL *string        `json:"externalUrl"`
	DownloadURL string         `json:"downloadUrl"`
}

type hangarFileInfo struct {
	Name       string `json:"name"`
	SizeBytes  int64  `json:"sizeBytes"`
	SHA256Hash string `json:"sha256Hash"`
}

type hangarPluginDependency struct{}

type HangarPlatformVersion struct {
	Version     string   `json:"version"`
	SubVersions []string `json:"subVersions"`
}
