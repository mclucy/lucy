package exttype

// FileSpongePluginsIdentifier represents the structure of
// META-INF/sponge_plugins.json files found in Sponge plugin JARs.
//
// Docs: https://docs.spongepowered.org/stable/en/plugin/plugin-meta.html
type FileSpongePluginsIdentifier struct {
	Loader struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"loader"`
	License string                     `json:"license"`
	Global  FileSpongePluginMetadata   `json:"global"`
	Plugins []FileSpongePluginMetadata `json:"plugins"`
}

type FileSpongePluginMetadata struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Entrypoint  string `json:"entrypoint"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Links       struct {
		Homepage string `json:"homepage"`
		Source   string `json:"source"`
		Issues   string `json:"issues"`
	} `json:"links"`
	Contributors []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	} `json:"contributors"`
	Dependencies []struct {
		ID        string `json:"id"`
		Version   string `json:"version"`
		LoadOrder string `json:"load-order"`
		Optional  bool   `json:"optional"`
	} `json:"dependencies"`
}
