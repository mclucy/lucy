package exttype

// FileForgeModIdentifier is for 1.13+ forge & neoforge. This is a toml file.
type FileForgeModIdentifier struct {
	ModLoader       string                            `toml:"modLoader"`
	LoaderVersion   string                            `toml:"loaderVersion"`
	IssueTrackerURL string                            `toml:"issueTrackerURL"`
	LogoFile        string                            `toml:"logoFile"`
	License         string                            `toml:"license"`
	Mods            []forgeModInfo                    `toml:"mods"`
	Dependencies    map[string][]forgeModDependencies `toml:"dependencies"`
	ModProperties   map[string]string                 `toml:"-"` // ignored
}

type forgeModInfo struct {
	ModID         string `toml:"modId"`
	Version       string `toml:"version"`
	DisplayName   string `toml:"displayName"`
	ItemIcon      string `toml:"itemIcon"`
	DisplayURL    string `toml:"displayURL"`
	UpdateJSONURL string `toml:"updateJSONURL"`
	LogoFile      string `toml:"logoFile"`
	Credits       string `toml:"credits"`
	Authors       string `toml:"authors"`
	Description   string `toml:"description"`
}

type forgeModDependencies struct {
	ModID        string `toml:"modId"`
	Mandatory    bool   `toml:"mandatory"`
	VersionRange string `toml:"versionRange"`
	Ordering     string `toml:"ordering"`
	Side         string `toml:"side"`
}

// FileForgeModIdentifierOld is for 1.12 and older forge mods. This is a json file.
type FileForgeModIdentifierOld []struct {
	ModId        string        `json:"modid"`
	Name         string        `json:"name"`
	Description  string        `json:"description"`
	Version      string        `json:"version"`
	McVersion    string        `json:"mcversion"`
	URL          string        `json:"url"`
	UpdateURL    string        `json:"updateUrl"`
	AuthorList   []string      `json:"authorList"`
	Credits      string        `json:"credits"`
	LogoFile     string        `json:"logoFile"`
	Screenshots  []interface{} `json:"screenshots"`
	Dependencies []interface{} `json:"dependencies"`
}
