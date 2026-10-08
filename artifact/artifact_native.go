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

	"github.com/mclucy/lucy/types"
	"github.com/pelletier/go-toml"
	"gopkg.in/yaml.v3"
)

// Inspect keeps each descriptor identity and nested container location intact.
func Inspect(ctx context.Context, filename string, loader types.Ecosystem) ([]types.NativeModule, error) {
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

func inspectArchive(z *zip.Reader, loader types.Ecosystem, member string, depth int) ([]types.NativeModule, error) {
	if depth > 16 {
		return nil, fmt.Errorf("nested archive depth exceeds 16")
	}
	switch loader {
	case types.EcoFabric:
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
		if d.Environment == "client" {
			return nil, nil
		}
		m := types.NativeModule{ID: d.ID, Version: d.Version, Loader: loader, Member: member, Descriptor: "fabric.mod.json", Provides: d.Provides, Environment: d.Environment, Dependencies: []types.NativeDependency{}}
		for kind, deps := range map[string]map[string]jsontext.Value{"required": d.Depends, "recommended": d.Recommends, "suggested": d.Suggests, "incompatible": d.Breaks, "conflict": d.Conflicts} {
			for id, raw := range deps {
				var one string
				var many []string
				if err := json.Unmarshal(raw, &one); err == nil {
					many = []string{one}
				} else if err := json.Unmarshal(raw, &many); err != nil {
					return nil, err
				}
				m.Dependencies = append(m.Dependencies, types.NativeDependency{ID: id, Kind: kind, Constraint: strings.Join(many, " || ")})
			}
		}
		out := []types.NativeModule{m}
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
	case types.EcoForge, types.EcoNeoforge:
		name := "META-INF/mods.toml"
		if loader == types.EcoNeoforge {
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
		if raw == nil {
			return nil, nil
		}
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
		out := []types.NativeModule{}
		for _, mod := range d.Mods {
			v := mod.Version
			if v == "${file.jarVersion}" {
				data, err := nativeEntry(z, "META-INF/MANIFEST.MF")
				if err != nil {
					return nil, err
				}
				v = string(forgeManifestVersion(string(data)))
			}
			m := types.NativeModule{ID: mod.ID, Version: v, Loader: loader, Member: member, Descriptor: name, Dependencies: []types.NativeDependency{}}
			for _, dep := range d.Dependencies[mod.ID] {
				if strings.EqualFold(dep.Side, "CLIENT") {
					continue
				}
				kind := dep.Type
				if kind == "" {
					kind = "optional"
					if dep.Mandatory {
						kind = "required"
					}
				}
				if kind == "discouraged" {
					kind = "conflict"
				}
				m.Dependencies = append(m.Dependencies, types.NativeDependency{ID: dep.ID, Constraint: dep.Range, Kind: kind, Ordering: dep.Ordering})
			}
			out = append(out, m)
		}
		if loader == types.EcoNeoforge {
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
	case types.EcoBukkit, types.EcoPaper:
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
		m := types.NativeModule{ID: d.Name, Version: d.Version, Loader: types.EcoBukkit, Member: member, Descriptor: name, APIVersion: d.API, FoliaSupported: d.Folia, Provides: d.Provides, Dependencies: []types.NativeDependency{}}
		for _, id := range d.Depend {
			m.Dependencies = append(m.Dependencies, types.NativeDependency{ID: id, Kind: "required", Ordering: "AFTER"})
		}
		for _, id := range d.SoftDepend {
			m.Dependencies = append(m.Dependencies, types.NativeDependency{ID: id, Kind: "optional", Ordering: "AFTER"})
		}
		for _, id := range d.LoadBefore {
			m.Dependencies = append(m.Dependencies, types.NativeDependency{ID: id, Kind: "optional", Ordering: "BEFORE"})
		}
		for phase, deps := range d.Dependencies {
			for id, dep := range deps {
				kind := "required"
				if dep.Required != nil && !*dep.Required {
					kind = "optional"
				}
				join := dep.JoinClasspath == nil || *dep.JoinClasspath
				m.Dependencies = append(m.Dependencies, types.NativeDependency{ID: id, Kind: kind, Phase: phase, Load: dep.Load, JoinClasspath: join})
			}
		}
		return []types.NativeModule{m}, nil
	case types.EcoMcdr:
		raw, err := nativeEntry(z, "mcdreforged.plugin.json")
		if err != nil {
			return nil, err
		}
		if raw == nil {
			return nil, nil
		}
		return nativeMCDR(raw, member)
	default:
		return nil, fmt.Errorf("unsupported native loader %s", loader)
	}
}

func nativeMCDR(raw []byte, member string) ([]types.NativeModule, error) {
	var d struct {
		ID           string            `json:"id"`
		Version      string            `json:"version"`
		Dependencies map[string]string `json:"dependencies"`
		Requirements []string          `json:"requirements"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, err
	}
	m := types.NativeModule{ID: d.ID, Version: d.Version, Loader: types.EcoMcdr, Member: member, Descriptor: "mcdreforged.plugin.json", Dependencies: []types.NativeDependency{}}
	for id, rangeText := range d.Dependencies {
		m.Dependencies = append(m.Dependencies, types.NativeDependency{ID: id, Kind: "required", Constraint: rangeText})
	}
	for _, r := range d.Requirements {
		m.Dependencies = append(m.Dependencies, types.NativeDependency{ID: r, Kind: "python"})
	}
	return []types.NativeModule{m}, nil
}

func inspectPython(ctx context.Context, p string) ([]types.NativeModule, error) {
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
	return nativeMCDR(data, "")
}

func ModuleProvides(m types.NativeModule, id string) bool {
	if m.Loader == types.EcoBukkit {
		return strings.EqualFold(m.ID, id) || slices.ContainsFunc(m.Provides, func(p string) bool { return strings.EqualFold(p, id) })
	}
	return m.ID == id || slices.Contains(m.Provides, id)
}
