package fileschema

import "github.com/pelletier/go-toml/v2"

// FileModLoaderAuthors accepts the documented string form and the array form
// some published jars use. Normalization converts a string to a one-element
// slice before decoding.
type FileModLoaderAuthors []string

// FileModLoaderIdentifier is for 1.13+ forge & neoforge mods. This is a toml file.
type FileModLoaderIdentifier struct {
	ModLoader       string                          `toml:"modLoader"`
	LoaderVersion   string                          `toml:"loaderVersion"`
	IssueTrackerURL string                          `toml:"issueTrackerURL"`
	LogoFile        string                          `toml:"logoFile"`
	License         string                          `toml:"license"`
	Mods            []forgeModInfo                  `toml:"mods"`
	Dependencies    map[string][]ForgeModDependency `toml:"dependencies"`
	ModProperties   map[string]string               `toml:"-"` // ignored
}

// ParseFileModLoaderIdentifier decodes a Forge-family descriptor. Published
// descriptors deviate from the documented shapes in two tolerated ways: an
// ownerless top-level [[dependencies]] array and an authors array. The decoder
// normalizes those shapes before go-toml decodes the typed value.
func ParseFileModLoaderIdentifier(data []byte) (FileModLoaderIdentifier, error) {
	normalized, err := normalizeForgeDescriptor(data)
	if err != nil {
		return FileModLoaderIdentifier{}, err
	}
	var identifier FileModLoaderIdentifier
	if err := toml.Unmarshal(normalized, &identifier); err != nil {
		return FileModLoaderIdentifier{}, err
	}
	return identifier, nil
}

// normalizeForgeDescriptor rewrites the descriptor shapes FML tolerates into
// the documented shapes. FML reads dependencies only as dependencies.<modId>
// and never reads authors, so an ownerless dependency array contributes no
// dependencies and an authors array carries no server behavior.
func normalizeForgeDescriptor(data []byte) ([]byte, error) {
	var document map[string]any
	if err := toml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	normalizeForgeDependencies(document)
	normalizeForgeAuthors(document)
	return toml.Marshal(document)
}

func normalizeForgeDependencies(document map[string]any) {
	dependencies, ok := document["dependencies"].(map[string]any)
	if !ok {
		delete(document, "dependencies")
		return
	}
	for owner, value := range dependencies {
		if _, ok := value.([]any); !ok {
			delete(dependencies, owner)
		}
	}
	if len(dependencies) == 0 {
		delete(document, "dependencies")
	}
}

func normalizeForgeAuthors(document map[string]any) {
	mods, ok := document["mods"].([]any)
	if !ok {
		return
	}
	for _, entry := range mods {
		mod, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		switch authors := mod["authors"].(type) {
		case string:
			mod["authors"] = []any{authors}
		case []any:
			kept := make([]any, 0, len(authors))
			for _, author := range authors {
				if _, ok := author.(string); ok {
					kept = append(kept, author)
				}
			}
			if len(kept) == 0 {
				delete(mod, "authors")
				continue
			}
			mod["authors"] = kept
		case nil:
		default:
			delete(mod, "authors")
		}
	}
}

type forgeModInfo struct {
	ModID         string               `toml:"modId"`
	Version       string               `toml:"version"`
	DisplayName   string               `toml:"displayName"`
	ItemIcon      string               `toml:"itemIcon"`
	DisplayURL    string               `toml:"displayURL"`
	UpdateJSONURL string               `toml:"updateJSONURL"`
	LogoFile      string               `toml:"logoFile"`
	Credits       string               `toml:"credits"`
	Authors       FileModLoaderAuthors `toml:"authors"`
	Description   string               `toml:"description"`
}

// ForgeModDependency is one dependency a Forge-family descriptor declares.
type ForgeModDependency struct {
	ModID        string `toml:"modId"`
	Mandatory    bool   `toml:"mandatory"`
	Type         string `toml:"type"`
	VersionRange string `toml:"versionRange"`
	Ordering     string `toml:"ordering"`
	Side         string `toml:"side"`
}

// FileForgeModIdentifierOld is the JSON metadata shape used by Forge 1.12
// and earlier.
type FileForgeModIdentifierOld []struct {
	ModId        string   `json:"modid"`
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Version      string   `json:"version"`
	McVersion    string   `json:"mcversion"`
	URL          string   `json:"url"`
	UpdateURL    string   `json:"updateUrl"`
	AuthorList   []string `json:"authorList"`
	Credits      string   `json:"credits"`
	LogoFile     string   `json:"logoFile"`
	Screenshots  []any    `json:"screenshots"`
	Dependencies []any    `json:"dependencies"`
}
