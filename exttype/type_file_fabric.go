package exttype

import "github.com/mclucy/lucy/tools"

type FabricEnvironment string

const (
	FabricEnvironmentClient FabricEnvironment = "client"
	FabricEnvironmentServer FabricEnvironment = "server"
	FabricEnvironmentAny    FabricEnvironment = "*"
)

// FileFabricModIdentifier represents the structure of fabric.mod.json files found
// in Fabric mods' `.jar` files.
//
// Docs: https://fabricmc.net/wiki/documentation:fabric_mod_json_spec
type FileFabricModIdentifier struct {
	SchemaVersion int      `json:"schemaVersion"`
	Id            string   `json:"id"`
	Version       string   `json:"version"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	Authors       []string `json:"authors"`

	// Fields officially supported:
	//   - "email"
	//   - "homepage"
	//   - "irc"
	//   - "issues"
	//   - "sources"
	Contact map[string]string `json:"contact"`

	// This uses the SPDX format https://spdx.org/licenses/
	// TODO: Should implement and check whether other platforms use this too.
	License string `json:"license"`

	Icon        string            `json:"icon"`
	Environment FabricEnvironment `json:"environment"`
	Jars        []struct {
		File string `json:"file"`
	} `json:"-"`
	Entrypoints      map[string][]string `json:"-"`
	Mixins           []string            `json:"-"`
	AccessWidener    string              `json:"accessWidener"`
	LanguageAdapters map[string]string   `json:"-"`

	// Depends > Recommends > Suggests
	// Breaks > Conflicts
	Depends    map[string]tools.SingleOrSlice[string] `json:"depends"`
	Recommends map[string]tools.SingleOrSlice[string] `json:"recommends"`
	Suggests   map[string]tools.SingleOrSlice[string] `json:"suggests"`
	Breaks     map[string]tools.SingleOrSlice[string] `json:"breaks"`
	Conflicts  map[string]tools.SingleOrSlice[string] `json:"conflicts"`

	Custom interface{} `json:"-"`
}

type FileFabricModIdentifierOld struct {
	// TODO: See https://wiki.fabricmc.net/documentation:fabric_mod_json_spec
	// This is for very old fabric (< 0.4.0). It does not matter much right
	// now. Besides, it is poorly documented.
	//
	// When SchemaVersion is 0 or missing, it is considered old.
}
