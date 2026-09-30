package buildrepo

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/pelletier/go-toml"
)

type catalogDocument struct {
	Plugins   map[string]catalogEntry `toml:"plugins"`
	Libraries map[string]catalogEntry `toml:"libraries"`
}

type catalogEntry struct {
	ID     string `toml:"id"`
	Module string `toml:"module"`
}

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
	for _, section := range []map[string]catalogEntry{document.Plugins, document.Libraries} {
		for alias, entry := range section {
			if id := pluginIDOfEntry(entry); id != "" {
				values[alias] = id
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
