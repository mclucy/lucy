package resolve

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	serverpkg "github.com/mclucy/lucy/server"

	versionpkg "github.com/mclucy/lucy/version"
)

// moduleKey scopes a module identity so identically named modules in
// different environments or ecosystems stay distinct.
type moduleKey struct {
	environment serverpkg.Environment
	ecosystem   catalog.Ecosystem
	id          string
}

// moduleSource names the artifact a module came from. The offsets identify the
// declaration inside its package; runtime modules have no package.
type moduleSource struct {
	packageIndex int
	moduleIndex  int
}

func (s moduleSource) owns(packageIndex, moduleIndex int) bool {
	return s.packageIndex == packageIndex && s.moduleIndex == moduleIndex
}

func (s moduleSource) runtime() bool {
	return s.packageIndex < 0
}

// moduleProvider is the module that answers one identity, together with the
// coordinates a dependency binds to.
type moduleProvider struct {
	source    moduleSource
	module    catalog.DescriptorModule
	provider  catalog.Provider
	projectID string
}

// finalizeModules checks module requirements against the target and drops
// modules that lose to another selected module with the same identity. It
// returns the problems it found; only an unparsable or ambiguous module
// identity is an error.
func finalizeModules(packages []document.Package, target Target) ([]ModuleProblem, error) {
	index, err := buildModuleIndex(packages)
	if err != nil {
		return nil, err
	}
	runtime := runtimeModules(target)
	var problems []ModuleProblem
	surviving := make([][]catalog.DescriptorModule, len(packages))
	for pi := range packages {
		p := &packages[pi]
		if !p.Enabled() {
			continue
		}
		for mi := range p.Modules {
			m := p.Modules[mi]
			key := moduleKey{
				p.Environment,
				p.Ecosystem,
				normalizeModuleID(m.Ecosystem, m.ID),
			}
			if owner, ok := index[key]; !ok || !owner.source.owns(pi, mi) {
				continue
			}
			problems = append(
				problems,
				checkModuleDependencies(p, m, target, index, runtime)...,
			)
			surviving[pi] = append(surviving[pi], m)
		}
	}
	// Assignment happens only after every package has been checked, so a
	// lookup never observes a compacted module list.
	for pi := range packages {
		if !packages[pi].Enabled() {
			continue
		}
		packages[pi].Modules = surviving[pi]
	}
	return problems, nil
}

// buildModuleIndex resolves every module identity to the declaration that
// answers it. A native module outranks a bundled copy of the same identity;
// two bundled copies compare versions. Two native modules in different
// packages are ambiguous.
func buildModuleIndex(packages []document.Package) (map[moduleKey]moduleProvider, error) {
	index := map[moduleKey]moduleProvider{}
	for pi := range packages {
		p := &packages[pi]
		if !p.Enabled() {
			continue
		}
		for mi, m := range p.Modules {
			candidate := moduleProvider{
				source:    moduleSource{packageIndex: pi, moduleIndex: mi},
				module:    m,
				provider:  p.Provider,
				projectID: p.ProjectID,
			}
			names := append([]string{m.ID}, m.Provides...)
			for _, name := range names {
				key := moduleKey{
					p.Environment,
					p.Ecosystem,
					normalizeModuleID(m.Ecosystem, name),
				}
				previous, ok := index[key]
				if !ok {
					index[key] = candidate
					continue
				}
				keep, err := moduleWins(
					previous,
					candidate,
					packages[previous.source.packageIndex].Filename,
					p.Filename,
					name,
				)
				if err != nil {
					return nil, err
				}
				if !keep {
					index[key] = candidate
				}
			}
		}
	}
	return index, nil
}

// moduleWins reports whether the module already indexed keeps the identity.
func moduleWins(
	previous moduleProvider,
	candidate moduleProvider,
	previousFile string,
	candidateFile string,
	name string,
) (bool, error) {
	previousNative := previous.module.NestedPath == ""
	candidateNative := candidate.module.NestedPath == ""
	if previousNative && candidateNative {
		if previous.source.packageIndex == candidate.source.packageIndex {
			return true, nil
		}
		return false, fmt.Errorf(
			"duplicate native module %s in %s and %s",
			name,
			candidateFile,
			previousFile,
		)
	}
	if previousNative {
		return true, nil
	}
	if candidateNative {
		return false, nil
	}
	a, err := versionpkg.ParseVersion(
		previous.module.Version,
		previous.module.VersionDialect,
	)
	if err != nil {
		return false, err
	}
	b, err := versionpkg.ParseVersion(
		candidate.module.Version,
		candidate.module.VersionDialect,
	)
	if err != nil {
		return false, err
	}
	comparison, comparable := versionpkg.Compare(a, b)
	if !comparable {
		return false, fmt.Errorf(
			"incomparable nested module versions %s and %s",
			previous.module.Version,
			candidate.module.Version,
		)
	}
	return comparison >= 0, nil
}

// runtimeModules reports the modules the runtime itself provides. They answer
// requirements before any package does, the same way a loader resolves its own
// modules before the package graph.
func runtimeModules(target Target) map[string]catalog.DescriptorModule {
	modules := map[string]catalog.DescriptorModule{}
	add := func(module catalog.DescriptorModule) {
		id := normalizeModuleID(module.Ecosystem, module.ID)
		if id == "" {
			return
		}
		if _, ok := modules[id]; ok {
			return
		}
		modules[id] = module
	}
	add(catalog.DescriptorModule{
		ID:        "minecraft",
		Version:   target.Minecraft,
		Ecosystem: catalog.EcosystemMinecraft,
	})
	switch target.Product {
	case serverpkg.ProductFabric:
		add(catalog.DescriptorModule{
			ID:        "fabricloader",
			Version:   target.CoreVersion,
			Ecosystem: catalog.EcosystemFabric,
		})
	case serverpkg.ProductForge:
		add(catalog.DescriptorModule{
			ID:        "forge",
			Version:   target.CoreVersion,
			Ecosystem: catalog.EcosystemForge,
		})
	case serverpkg.ProductNeoForge:
		add(catalog.DescriptorModule{
			ID:        "neoforge",
			Version:   target.CoreVersion,
			Ecosystem: catalog.EcosystemNeoForge,
		})
	}
	if target.MCDRVersion != "" {
		add(catalog.DescriptorModule{
			ID:        "mcdreforged",
			Version:   versionpkg.Version(target.MCDRVersion),
			Ecosystem: catalog.EcosystemMCDR,
		})
	}
	for _, module := range target.CoreModules {
		add(module)
	}
	return modules
}

func normalizeModuleID(ecosystem catalog.Ecosystem, id string) string {
	if ecosystem == catalog.EcosystemBukkit || ecosystem == catalog.EcosystemPaper {
		return strings.ToLower(id)
	}
	return id
}

func checkModuleDependencies(
	p *document.Package,
	m catalog.DescriptorModule,
	target Target,
	index map[moduleKey]moduleProvider,
	runtime map[string]catalog.DescriptorModule,
) []ModuleProblem {
	var problems []ModuleProblem
	for di := range m.Dependencies {
		dep := &m.Dependencies[di]
		// A Java requirement is checked against the Java runtime that runs
		// the server, and only warns.
		if dep.Kind == catalog.DependencyKindPython || dep.ID == "java" {
			continue
		}
		value := ""
		var owner *moduleProvider
		if module, ok := runtime[normalizeModuleID(p.Ecosystem, dep.ID)]; ok {
			value = string(module.Version)
		} else {
			key := moduleKey{
				p.Environment,
				p.Ecosystem,
				normalizeModuleID(p.Ecosystem, dep.ID),
			}
			if provider, ok := index[key]; ok {
				owner = &provider
				value = string(provider.module.Version)
			}
		}
		matched := value != ""
		invalid := false
		if fmlLoader(target) && value == "0.0NONE" {
			// FMLLoader treats the 0.0NONE sentinel as satisfying every range.
			matched = true
		} else if matched && dep.Constraint != "" {
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
		if !invalid && dep.Constraint != "" && !matched {
			compatible, err := matchesLoaderCompatibility(
				dep.Constraint,
				dep.ID,
				target,
				m.VersionDialect,
			)
			if err != nil {
				problems = append(problems, moduleProblem(p, fmt.Sprintf("module %s requires %s %s: %v", m.ID, dep.ID, dep.Constraint, err)))
				invalid = true
			} else {
				matched = compatible
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
			if owner != nil && !owner.source.runtime() {
				dep.Provider = owner.provider
				dep.ProjectID = owner.projectID
				dep.ModuleID = owner.module.ID
			} else {
				dep.ModuleID = dep.ID
			}
		}
	}
	return problems
}
