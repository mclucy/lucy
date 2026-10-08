package buildrepo

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml"
)

type catalogDocument struct {
	Plugins   map[string]any `toml:"plugins"`
	Libraries map[string]any `toml:"libraries"`
}

type catalogEntry struct {
	ID     string
	Module string
}

// catalogAlias rewrites the separators Gradle treats as equivalent in accessor
// keys: fabric_loom, fabric.loom, and fabric-loom all read as
// libs.plugins.fabric.loom.
var catalogAlias = strings.NewReplacer("_", "-", ".", "-")

// indexCatalogs reads every libs.versions.toml reachable from the builds,
// mapping catalog aliases to plugin ids.
func (p *prober) indexCatalogs() {
	for _, build := range p.builds {
		for current := build.Dir; ; current = filepath.Dir(current) {
			p.readCatalog(filepath.Join(current, "gradle", "libs.versions.toml"))
			if current == p.root {
				break
			}
		}
	}
}

// readCatalog parses one libs.versions.toml into the alias to plugin id map.
func (p *prober) readCatalog(path string) {
	if _, seen := p.catalogs[path]; seen {
		return
	}
	p.catalogs[path] = nil
	content, ok := p.readOptional(path)
	if !ok {
		return
	}
	var document catalogDocument
	if err := toml.Unmarshal([]byte(content), &document); err != nil {
		p.findings.add(path, 0, "version catalog could not be parsed: "+err.Error())
		return
	}
	values := map[string]string{}
	for _, section := range []struct {
		plugins bool
		entries map[string]any
	}{
		{plugins: true, entries: document.Plugins},
		{entries: document.Libraries},
	} {
		for alias, raw := range section.entries {
			if id := pluginIDOfEntry(catalogEntryOf(section.plugins, raw)); id != "" {
				values[catalogAlias.Replace(alias)] = id
			}
		}
	}
	p.catalogs[path] = values
	for alias, id := range values {
		if existing, ok := p.catalog[alias]; ok && existing != id {
			continue
		}
		p.catalog[alias] = id
	}
}

// catalogEntryOf reduces one catalog entry to the fields Lucy reads. Gradle
// accepts every entry as a table or as a short string: a plugin string is
// "id:version" and a library string is "group:name[:version]"; anything else,
// such as a nested sub-table, carries no entry.
func catalogEntryOf(plugins bool, raw any) catalogEntry {
	var entry catalogEntry
	switch value := raw.(type) {
	case string:
		if plugins {
			entry.ID, _, _ = strings.Cut(value, ":")
			return entry
		}
		group, rest, found := strings.Cut(value, ":")
		if !found {
			return entry
		}
		name, _, _ := strings.Cut(rest, ":")
		entry.Module = group + ":" + name
	case map[string]any:
		entry.ID, _ = value["id"].(string)
		entry.Module, _ = value["module"].(string)
		if entry.Module == "" {
			group, _ := value["group"].(string)
			name, _ := value["name"].(string)
			entry.Module = group + ":" + name
		}
	}
	return entry
}

// pluginIDOfEntry derives a plugin id from a catalog entry, covering both the
// plugins table and libraries holding plugin marker modules.
func pluginIDOfEntry(entry catalogEntry) string {
	if entry.ID != "" {
		return entry.ID
	}
	group, name, found := strings.Cut(entry.Module, ":")
	if !found {
		return ""
	}
	marker, isMarker := strings.CutSuffix(name, ".gradle.plugin")
	if !isMarker || marker != group {
		return ""
	}
	return group
}

// sortBuild orders the derived collections of a build so that repeated probes
// of the same repository produce identical output.
func sortBuild(build *GradleBuild) {
	slices.SortFunc(build.Projects, func(a, b Project) int {
		return strings.Compare(a.Path, b.Path)
	})
	slices.Sort(build.IncludedBuilds)
	for i := range build.Projects {
		slices.Sort(build.Projects[i].Ecosystems)
	}
}
