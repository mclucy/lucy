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

// normalizeForgeDescriptor projects the fields the schema reads through the
// same accessors FML uses. FML reads a descriptor with NightConfig: a path
// resolves through tables only, so a list at an intermediate segment makes the
// path absent, while getConfigList requires a list at its final path and
// getConfigElement rejects a list of tables at a single-element path. The
// projection keeps only the schema fields and coerces list/scalar shapes, so
// go-toml decodes the documented struct.
func normalizeForgeDescriptor(data []byte) ([]byte, error) {
	var document map[string]any
	if err := toml.Unmarshal(data, &document); err != nil {
		return nil, err
	}
	normalized := map[string]any{}
	copyForgeStrings(normalized, document,
		"modLoader", "loaderVersion", "issueTrackerURL", "logoFile", "license")
	mods := normalizeForgeMods(document)
	if len(mods) == 0 {
		return toml.Marshal(normalized)
	}
	normalized["mods"] = mods
	if dependencies := normalizeForgeDependencies(document, mods); len(dependencies) > 0 {
		normalized["dependencies"] = dependencies
	}
	return toml.Marshal(normalized)
}

// configList mirrors NightConfigWrapper.getConfigList: the path resolves
// through tables and its final value must be a list. A non-list final value is
// a malformed descriptor; the projection drops it.
func configList(document map[string]any, path ...string) ([]any, bool) {
	value, ok := configPath(document, path...)
	if !ok {
		return nil, false
	}
	list, ok := value.([]any)
	return list, ok
}

// configPath mirrors NightConfig's path lookup. A list at an intermediate
// segment is not a table, so the path does not exist. That is how FML ignores
// an ownerless [[dependencies]] array when it reads dependencies.<modId>.
func configPath(document map[string]any, path ...string) (any, bool) {
	var current any = document
	for _, key := range path {
		table, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		current, ok = table[key]
		if !ok {
			return nil, false
		}
	}
	return current, true
}

func normalizeForgeMods(document map[string]any) []any {
	entries, ok := configList(document, "mods")
	if !ok {
		return nil
	}
	mods := make([]any, 0, len(entries))
	for _, entry := range entries {
		table, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		mod := map[string]any{}
		copyForgeStrings(mod, table,
			"modId", "version", "displayName", "itemIcon", "displayURL",
			"updateJSONURL", "logoFile", "credits", "description")
		if authors := normalizeForgeAuthors(table["authors"]); len(authors) > 0 {
			mod["authors"] = authors
		}
		if len(mod) > 0 {
			mods = append(mods, mod)
		}
	}
	return mods
}

// normalizeForgeAuthors accepts the documented string form and the array form
// some published jars use. FML never reads authors, so an unknown shape carries
// no server behavior and is dropped.
func normalizeForgeAuthors(value any) []any {
	switch authors := value.(type) {
	case string:
		return []any{authors}
	case []any:
		kept := make([]any, 0, len(authors))
		for _, author := range authors {
			if _, ok := author.(string); ok {
				kept = append(kept, author)
			}
		}
		return kept
	default:
		return nil
	}
}

// normalizeForgeDependencies reads dependencies.<modId> for every declared mod,
// matching ModInfo's getConfigList call. An ownerless dependencies list, or any
// other non-table dependencies value, yields no path and contributes nothing.
func normalizeForgeDependencies(document map[string]any, mods []any) map[string]any {
	dependencies := map[string]any{}
	for _, entry := range mods {
		mod, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		modID, ok := mod["modId"].(string)
		if !ok || modID == "" {
			continue
		}
		entries, ok := configList(document, "dependencies", modID)
		if !ok {
			continue
		}
		normalized := normalizeForgeDependencyEntries(entries)
		if len(normalized) > 0 {
			dependencies[modID] = normalized
		}
	}
	return dependencies
}

func normalizeForgeDependencyEntries(entries []any) []any {
	normalized := make([]any, 0, len(entries))
	for _, entry := range entries {
		table, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		dependency := map[string]any{}
		copyForgeStrings(dependency, table,
			"modId", "type", "versionRange", "ordering", "side")
		if mandatory, ok := table["mandatory"].(bool); ok {
			dependency["mandatory"] = mandatory
		}
		if len(dependency) > 0 {
			normalized = append(normalized, dependency)
		}
	}
	return normalized
}

func copyForgeStrings(destination, source map[string]any, keys ...string) {
	for _, key := range keys {
		if value, ok := source[key].(string); ok {
			destination[key] = value
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
