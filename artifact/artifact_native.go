package artifact

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/version"
	"github.com/pelletier/go-toml"
	"gopkg.in/yaml.v3"
)

// ReadModules keeps each descriptor identity and nested container location
// intact.
func ReadModules(ctx context.Context, filename string, loader catalog.Ecosystem) ([]catalog.DescriptorModule, error) {
	if strings.EqualFold(filepath.Ext(filename), ".py") {
		return inspectPython(ctx, filename)
	}
	z, err := zip.OpenReader(filename)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	return inspectArchive(&z.Reader, loader, "", 0)
}

func nativeEntry(z *zip.Reader, name string) ([]byte, error) {
	for _, f := range z.File {
		if f.Name == name {
			if f.UncompressedSize64 > 128<<20 {
				return nil, fmt.Errorf("oversized native descriptor or nested JAR %s", name)
			}
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer r.Close()
			return io.ReadAll(io.LimitReader(r, 128<<20))
		}
	}
	return nil, nil
}

func inspectArchive(z *zip.Reader, loader catalog.Ecosystem, member string, depth int) ([]catalog.DescriptorModule, error) {
	if depth > 16 {
		return nil, fmt.Errorf("nested archive depth exceeds 16")
	}
	switch loader {
	case catalog.EcosystemFabric:
		raw, err := nativeEntry(z, "fabric.mod.json")
		if err != nil {
			return nil, err
		}
		if raw == nil {
			return nil, nil
		}
		var d struct {
			ID          string   `json:"id"`
			Version     string   `json:"version"`
			Environment string   `json:"environment"`
			Provides    []string `json:"provides"`
			Jars        []struct {
				File string `json:"file"`
			} `json:"jars"`
			Depends    map[string]jsontext.Value `json:"depends"`
			Recommends map[string]jsontext.Value `json:"recommends"`
			Suggests   map[string]jsontext.Value `json:"suggests"`
			Breaks     map[string]jsontext.Value `json:"breaks"`
			Conflicts  map[string]jsontext.Value `json:"conflicts"`
		}
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, err
		}
		side := catalog.ParseSide(d.Environment)
		if side == catalog.SideClient {
			return nil, nil
		}
		m := catalog.DescriptorModule{
			ID:             d.ID,
			Version:        version.Version(d.Version),
			Ecosystem:      loader,
			VersionDialect: version.DialectFabric,
			NestedPath:     member,
			DescriptorPath: "fabric.mod.json",
			Provides:       d.Provides,
			Side:           side,
			Dependencies:   []catalog.DescriptorDependency{},
		}
		for kind, deps := range map[catalog.DependencyKind]map[string]jsontext.Value{
			catalog.DependencyKindRequired:     d.Depends,
			catalog.DependencyKindRecommended:  d.Recommends,
			catalog.DependencyKindSuggested:    d.Suggests,
			catalog.DependencyKindIncompatible: d.Breaks,
			catalog.DependencyKindConflict:     d.Conflicts,
		} {
			for id, raw := range deps {
				var one string
				var many []string
				if err := json.Unmarshal(raw, &one); err == nil {
					many = []string{one}
				} else if err := json.Unmarshal(raw, &many); err != nil {
					return nil, err
				}
				m.Dependencies = append(m.Dependencies, catalog.DescriptorDependency{
					ID:         id,
					Kind:       kind,
					Constraint: strings.Join(many, " || "),
				})
			}
		}
		out := []catalog.DescriptorModule{m}
		for _, jar := range d.Jars {
			raw, err := nativeEntry(z, jar.File)
			if err != nil {
				return nil, err
			}
			if raw == nil {
				return nil, fmt.Errorf("missing nested Fabric JAR %s", jar.File)
			}
			nested, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
			if err != nil {
				return nil, err
			}
			p := jar.File
			if member != "" {
				p = member + "/" + p
			}
			mods, err := inspectArchive(nested, loader, p, depth+1)
			if err != nil {
				return nil, err
			}
			out = append(out, mods...)
		}
		return out, nil
	case catalog.EcosystemForge, catalog.EcosystemNeoForge:
		name := "META-INF/mods.toml"
		if loader == catalog.EcosystemNeoForge {
			if raw, err := nativeEntry(z, "META-INF/neoforge.mods.toml"); err != nil {
				return nil, err
			} else if raw != nil {
				name = "META-INF/neoforge.mods.toml"
			}
		}
		raw, err := nativeEntry(z, name)
		if err != nil {
			return nil, err
		}
		var out []catalog.DescriptorModule
		if raw != nil {
			var d struct {
				Mods []struct {
					ID      string `toml:"modId"`
					Version string `toml:"version"`
				} `toml:"mods"`
				Dependencies map[string][]struct {
					ID        string `toml:"modId"`
					Mandatory bool   `toml:"mandatory"`
					Type      string `toml:"type"`
					Range     string `toml:"versionRange"`
					Ordering  string `toml:"ordering"`
					Side      string `toml:"side"`
				} `toml:"dependencies"`
			}
			if err := toml.Unmarshal(raw, &d); err != nil {
				return nil, err
			}
			for _, mod := range d.Mods {
				v := version.Version(mod.Version)
				if v == "${file.jarVersion}" {
					data, err := nativeEntry(z, "META-INF/MANIFEST.MF")
					if err != nil {
						return nil, err
					}
					v = forgeManifestVersion(string(data))
				}
				m := catalog.DescriptorModule{
					ID:             mod.ID,
					Version:        v,
					Ecosystem:      loader,
					VersionDialect: version.DialectMaven,
					NestedPath:     member,
					DescriptorPath: name,
					Dependencies:   []catalog.DescriptorDependency{},
				}
				for _, dep := range d.Dependencies[mod.ID] {
					if catalog.ParseSide(dep.Side) == catalog.SideClient {
						continue
					}
					kind := catalog.ParseDependencyKind(dep.Type)
					if kind == catalog.DependencyKindUnknown {
						kind = catalog.DependencyKindOptional
						if dep.Mandatory {
							kind = catalog.DependencyKindRequired
						}
					}
					m.Dependencies = append(m.Dependencies, catalog.DescriptorDependency{
						ID:         dep.ID,
						Constraint: dep.Range,
						Kind:       kind,
						Ordering:   dep.Ordering,
					})
				}
				out = append(out, m)
			}
		}
		if loader == catalog.EcosystemNeoForge {
			// A jar without its own descriptor may still ship mods in JarJar
			// containers; FML loads those modules from the outer jar.
			data, err := nativeEntry(z, "META-INF/jarjar/metadata.json")
			if err != nil {
				return nil, err
			}
			if data != nil {
				var jj struct {
					Jars []struct {
						Path string `json:"path"`
					} `json:"jars"`
				}
				if err := json.Unmarshal(data, &jj); err != nil {
					return nil, err
				}
				for _, jar := range jj.Jars {
					raw, err := nativeEntry(z, jar.Path)
					if err != nil {
						return nil, err
					}
					if raw == nil {
						return nil, fmt.Errorf("missing JarJar container %s", jar.Path)
					}
					nested, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
					if err != nil {
						return nil, err
					}
					p := jar.Path
					if member != "" {
						p = member + "/" + p
					}
					mods, err := inspectArchive(nested, loader, p, depth+1)
					if err != nil {
						return nil, err
					}
					out = append(out, mods...)
				}
			}
		}
		return out, nil
	case catalog.EcosystemBukkit, catalog.EcosystemPaper:
		name := "paper-plugin.yml"
		raw, err := nativeEntry(z, name)
		if err != nil {
			return nil, err
		}
		if raw == nil {
			name = "plugin.yml"
			raw, err = nativeEntry(z, name)
			if err != nil {
				return nil, err
			}
		}
		if raw == nil {
			return nil, nil
		}
		var d struct {
			Name         string   `yaml:"name"`
			Version      string   `yaml:"version"`
			API          string   `yaml:"api-version"`
			Folia        bool     `yaml:"folia-supported"`
			Provides     []string `yaml:"provides"`
			Depend       []string `yaml:"depend"`
			SoftDepend   []string `yaml:"softdepend"`
			LoadBefore   []string `yaml:"loadbefore"`
			Dependencies map[string]map[string]struct {
				Required      *bool  `yaml:"required"`
				Load          string `yaml:"load"`
				JoinClasspath *bool  `yaml:"join-classpath"`
			} `yaml:"dependencies"`
		}
		if err := yaml.Unmarshal(raw, &d); err != nil {
			return nil, err
		}
		m := catalog.DescriptorModule{
			ID:             d.Name,
			Version:        version.Version(d.Version),
			Ecosystem:      catalog.EcosystemBukkit,
			VersionDialect: version.DialectOpaque,
			NestedPath:     member,
			DescriptorPath: name,
			APIVersion:     d.API,
			FoliaSupported: d.Folia,
			Provides:       d.Provides,
			Dependencies:   []catalog.DescriptorDependency{},
		}
		for _, id := range d.Depend {
			m.Dependencies = append(m.Dependencies, catalog.DescriptorDependency{
				ID:       id,
				Kind:     catalog.DependencyKindRequired,
				Ordering: "AFTER",
			})
		}
		for _, id := range d.SoftDepend {
			m.Dependencies = append(m.Dependencies, catalog.DescriptorDependency{
				ID:       id,
				Kind:     catalog.DependencyKindOptional,
				Ordering: "AFTER",
			})
		}
		for _, id := range d.LoadBefore {
			m.Dependencies = append(m.Dependencies, catalog.DescriptorDependency{
				ID:       id,
				Kind:     catalog.DependencyKindOptional,
				Ordering: "BEFORE",
			})
		}
		for phase, deps := range d.Dependencies {
			for id, dep := range deps {
				kind := catalog.DependencyKindRequired
				if dep.Required != nil && !*dep.Required {
					kind = catalog.DependencyKindOptional
				}
				join := dep.JoinClasspath == nil || *dep.JoinClasspath
				m.Dependencies = append(m.Dependencies, catalog.DescriptorDependency{
					ID:            id,
					Kind:          kind,
					Phase:         phase,
					Load:          dep.Load,
					JoinClasspath: join,
				})
			}
		}
		return []catalog.DescriptorModule{m}, nil
	case catalog.EcosystemMCDR:
		raw, err := nativeEntry(z, "mcdreforged.plugin.json")
		if err != nil {
			return nil, err
		}
		if raw == nil {
			return nil, nil
		}
		return nativeMCDR(z, raw, member)
	default:
		return nil, fmt.Errorf("unsupported native loader %s", loader)
	}
}

func nativeMCDR(
	z *zip.Reader,
	raw []byte,
	member string,
) ([]catalog.DescriptorModule, error) {
	var d struct {
		ID               string            `json:"id"`
		Version          string            `json:"version"`
		Dependencies     map[string]string `json:"dependencies"`
		Requirements     []string          `json:"requirements"`
		RequirementsFile jsontext.Value    `json:"requirements_file"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	m := catalog.DescriptorModule{
		ID:             d.ID,
		Version:        version.Version(d.Version),
		Ecosystem:      catalog.EcosystemMCDR,
		VersionDialect: version.DialectMCDR,
		NestedPath:     member,
		DescriptorPath: "mcdreforged.plugin.json",
		Dependencies:   []catalog.DescriptorDependency{},
	}
	for id, rangeText := range d.Dependencies {
		m.Dependencies = append(m.Dependencies, catalog.DescriptorDependency{
			ID:         id,
			Kind:       catalog.DependencyKindRequired,
			Constraint: rangeText,
		})
	}
	for _, r := range d.Requirements {
		m.Dependencies = append(m.Dependencies, catalog.DescriptorDependency{
			ID:   r,
			Kind: catalog.DependencyKindPython,
		})
	}
	if z != nil {
		files, err := mcdrRequirementFiles(d.RequirementsFile)
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			files = []string{"requirements.txt"}
		}
		for _, name := range files {
			content, err := nativeEntry(z, name)
			if err != nil {
				return nil, err
			}
			if content == nil {
				continue
			}
			for _, requirement := range mcdrRequirements(content) {
				m.Dependencies = append(m.Dependencies, catalog.DescriptorDependency{
					ID:   requirement,
					Kind: catalog.DependencyKindPython,
				})
			}
		}
	}
	return []catalog.DescriptorModule{m}, nil
}

func mcdrRequirementFiles(raw jsontext.Value) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		if strings.TrimSpace(one) == "" {
			return nil, nil
		}
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, fmt.Errorf("invalid MCDR requirements_file: %w", err)
	}
	return many, nil
}

func mcdrRequirements(content []byte) []string {
	var requirements []string
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		requirements = append(requirements, line)
	}
	return requirements
}

func inspectPython(ctx context.Context, p string) ([]catalog.DescriptorModule, error) {
	code := `import ast,json,sys
m=ast.parse(open(sys.argv[1],encoding="utf-8").read())
for n in m.body:
 if isinstance(n,ast.Assign) and any(isinstance(t,ast.Name) and t.id=="PLUGIN_METADATA" for t in n.targets):
  print(json.dumps(ast.literal_eval(n.value)));break
else: raise ValueError("missing static PLUGIN_METADATA")`
	data, err := exec.CommandContext(ctx, "python3", "-c", code, p).Output()
	if err != nil {
		return nil, fmt.Errorf("inspect Python plugin metadata: %w", err)
	}
	return nativeMCDR(nil, data, "")
}

func ModuleProvides(m catalog.DescriptorModule, id string) bool {
	if m.Ecosystem == catalog.EcosystemBukkit {
		return strings.EqualFold(m.ID, id) || slices.ContainsFunc(m.Provides, func(p string) bool { return strings.EqualFold(p, id) })
	}
	return m.ID == id || slices.Contains(m.Provides, id)
}
