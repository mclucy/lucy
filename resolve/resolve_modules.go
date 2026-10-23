package resolve

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/lockfile"
	serverpkg "github.com/mclucy/lucy/server"

	versionpkg "github.com/mclucy/lucy/version"
)

// finalizeModules checks module requirements against the target and prunes
// modules that lose to another selected module with the same identity. It
// returns the problems it found; only an unparsable or ambiguous module
// identity is an error.
func finalizeModules(packages []lockfile.Package, target Target) ([]ModuleProblem, error) {
	type selected struct{ packageIndex, moduleIndex int }
	index := map[struct {
		environment serverpkg.Environment
		ecosystem   catalog.Ecosystem
		id          string
	}]selected{}
	var problems []ModuleProblem
	for pi := range packages {
		p := &packages[pi]
		if !p.Enabled() {
			continue
		}
		for mi, m := range p.Modules {
			names := append([]string{m.ID}, m.Provides...)
			for _, name := range names {
				if m.Ecosystem == catalog.EcosystemBukkit || m.Ecosystem == catalog.EcosystemPaper {
					name = strings.ToLower(name)
				}
				key := struct {
					environment serverpkg.Environment
					ecosystem   catalog.Ecosystem
					id          string
				}{p.Environment, p.Ecosystem, name}
				if old, ok := index[key]; ok {
					previous := packages[old.packageIndex].Modules[old.moduleIndex]
					if m.NestedPath == "" && previous.NestedPath == "" && pi != old.packageIndex {
						return nil, fmt.Errorf("duplicate native module %s in %s and %s", name, p.Filename, packages[old.packageIndex].Filename)
					}
					if previous.NestedPath == "" {
						continue
					}
					if m.NestedPath != "" {
						a, err := versionpkg.ParseVersion(
							previous.Version,
							previous.VersionDialect,
						)
						if err != nil {
							return nil, err
						}
						b, err := versionpkg.ParseVersion(
							m.Version,
							m.VersionDialect,
						)
						if err != nil {
							return nil, err
						}
						comparison, isComparable := versionpkg.Compare(a, b)
						if !isComparable {
							return nil, fmt.Errorf("incomparable nested module versions %s and %s", previous.Version, m.Version)
						}
						if comparison >= 0 {
							continue
						}
					}
				}
				index[key] = selected{pi, mi}
			}
		}
	}
	for pi := range packages {
		p := &packages[pi]
		if !p.Enabled() {
			continue
		}
		var selectedModules []catalog.DescriptorModule
		for mi := range p.Modules {
			m := p.Modules[mi]
			id := m.ID
			if m.Ecosystem == catalog.EcosystemBukkit || m.Ecosystem == catalog.EcosystemPaper {
				id = strings.ToLower(id)
			}
			key := struct {
				environment serverpkg.Environment
				ecosystem   catalog.Ecosystem
				id          string
			}{p.Environment, p.Ecosystem, id}
			if match := index[key]; match.packageIndex != pi || match.moduleIndex != mi {
				continue
			}
			for di := range m.Dependencies {
				dep := &m.Dependencies[di]
				// A Java requirement is checked against the Java runtime that
				// runs the server, and only warns.
				if dep.Kind == catalog.DependencyKindPython || dep.ID == "java" {
					continue
				}
				value := ""
				builtin := true
				switch dep.ID {
				case "minecraft":
					value = string(target.Minecraft)
				case "fabricloader":
					if target.Product == serverpkg.ProductFabric {
						value = string(target.CoreVersion)
					}
				case "forge", "neoforge":
					if dep.ID == target.Product.String() {
						value = string(target.CoreVersion)
					}
				case "mcdreforged":
					value = target.MCDRVersion
				default:
					builtin = false
				}
				var owner *lockfile.Package
				var ownerModule *catalog.DescriptorModule
				if !builtin {
					name := dep.ID
					if p.Ecosystem == catalog.EcosystemBukkit || p.Ecosystem == catalog.EcosystemPaper {
						name = strings.ToLower(name)
					}
					if match, ok := index[struct {
						environment serverpkg.Environment
						ecosystem   catalog.Ecosystem
						id          string
					}{p.Environment, p.Ecosystem, name}]; ok {
						owner = &packages[match.packageIndex]
						ownerModule = &owner.Modules[match.moduleIndex]
						value = string(ownerModule.Version)
					}
				}
				matched := value != ""
				invalid := false
				if matched && dep.Constraint != "" {
					var err error
					matched, err = versionpkg.Match(
						dep.Constraint,
						versionpkg.Version(value),
						m.VersionDialect,
					)
					if err != nil {
						problems = append(problems, moduleProblem(p, fmt.Sprintf("module %s requires %s %s: %v", m.ID, dep.ID, dep.Constraint, err)))
						invalid = true
					}
				}
				requirement := strings.TrimSpace(dep.ID + " " + dep.Constraint)
				if !invalid && dep.Kind == catalog.DependencyKindRequired && !matched {
					if value == "" {
						problems = append(problems, moduleProblem(p, fmt.Sprintf("module %s requires %s, which no selected package provides", m.ID, requirement)))
					} else {
						problems = append(problems, moduleProblem(p, fmt.Sprintf("module %s requires %s (selected %s)", m.ID, requirement, value)))
					}
				}
				if !invalid && dep.Kind == catalog.DependencyKindIncompatible && matched {
					problems = append(problems, moduleProblem(p, fmt.Sprintf("module %s breaks %s %s", m.ID, dep.ID, value)))
				}
				if matched {
					if owner != nil {
						dep.Provider = owner.Provider
						dep.ProjectID = owner.ProjectID
						dep.ModuleID = ownerModule.ID
					} else {
						dep.ModuleID = dep.ID
					}
				}
			}
			selectedModules = append(selectedModules, m)
		}
		p.Modules = selectedModules
	}
	return problems, nil
}
