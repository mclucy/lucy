package fileschema

import (
	"bytes"

	"github.com/pelletier/go-toml/v2"
)

// FileModLoaderIdentifier is for 1.13+ forge & neoforge mods. This is a toml file.
type FileModLoaderIdentifier struct {
	ModLoader       string                    `toml:"modLoader"`
	LoaderVersion   string                    `toml:"loaderVersion"`
	IssueTrackerURL string                    `toml:"issueTrackerURL"`
	LogoFile        string                    `toml:"logoFile"`
	License         string                    `toml:"license"`
	Mods            []forgeModInfo            `toml:"mods"`
	Dependencies    FileModLoaderDependencies `toml:"dependencies"`
	ModProperties   map[string]string         `toml:"-"` // ignored
}

// ParseFileModLoaderIdentifier decodes a Forge-family descriptor. The tolerant
// field decoders require the unmarshaler interface, so callers must not decode
// this type with toml.Unmarshal directly.
func ParseFileModLoaderIdentifier(data []byte) (FileModLoaderIdentifier, error) {
	var identifier FileModLoaderIdentifier
	decoder := toml.NewDecoder(bytes.NewReader(data)).EnableUnmarshalerInterface()
	if err := decoder.Decode(&identifier); err != nil {
		return FileModLoaderIdentifier{}, err
	}
	return identifier, nil
}

// FileModLoaderDependencies holds dependencies keyed by the mod that declares
// them. FML reads only dependencies.<modId>; an ownerless top-level
// [[dependencies]] array is legal TOML and is ignored, as FML ignores it.
type FileModLoaderDependencies struct {
	byOwner map[string][]ForgeModDependency
}

// UnmarshalTOML implements the TOML unmarshaler interface. go-toml calls it
// once for the keyed table and once per ownerless array table.
func (d *FileModLoaderDependencies) UnmarshalTOML(data []byte) error {
	var byOwner map[string][]ForgeModDependency
	if err := toml.Unmarshal(data, &byOwner); err == nil {
		if d.byOwner == nil {
			d.byOwner = make(map[string][]ForgeModDependency, len(byOwner))
		}
		for owner, deps := range byOwner {
			d.byOwner[owner] = append(d.byOwner[owner], deps...)
		}
		return nil
	}
	var ownerless []ForgeModDependency
	_ = toml.Unmarshal(data, &ownerless)
	return nil
}

// For returns the dependencies the owner declares.
func (d FileModLoaderDependencies) For(owner string) []ForgeModDependency {
	return d.byOwner[owner]
}

// FileModLoaderAuthors accepts the string form FML documents and the array
// form some published jars use. FML never reads the field, so an unknown shape
// carries no meaning to the server and is ignored.
type FileModLoaderAuthors []string

// UnmarshalTOML implements the TOML unmarshaler interface.
func (a *FileModLoaderAuthors) UnmarshalTOML(data []byte) error {
	var wrapped struct {
		Value any `toml:"value"`
	}
	if err := toml.Unmarshal(append([]byte("value = "), data...), &wrapped); err != nil {
		return nil
	}
	*a = (*a)[:0]
	switch value := wrapped.Value.(type) {
	case string:
		*a = append(*a, value)
	case []any:
		for _, item := range value {
			if author, ok := item.(string); ok {
				*a = append(*a, author)
			}
		}
	}
	return nil
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
