package resolve

import (
	"fmt"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	serverpkg "github.com/mclucy/lucy/server"

	versionpkg "github.com/mclucy/lucy/version"
)

// clientOnlyIdentities collects module identities that only client-only
// declarations answer. A loader filters those candidates out of a dedicated
// server, so requirements they alone would answer are not failures. Each
// identity is scoped like module resolution, by the environment of the
// declaring package and the target's module namespace, so it exempts only
// requirements in that namespace.
func clientOnlyIdentities(
	packages []document.Package,
	target Target,
) map[moduleKey]struct{} {
	identities := map[moduleKey]struct{}{}
	add := func(
		environment serverpkg.Environment,
		module catalog.DescriptorModule,
	) {
		for _, name := range module.ClientOnly {
			id := normalizeModuleID(module.Ecosystem, name)
			if id != "" {
				identities[moduleKey{
					environment,
					target.moduleNamespace(module.Ecosystem),
					id,
				}] = struct{}{}
			}
		}
	}
	for pi := range packages {
		p := &packages[pi]
		if !p.Enabled() {
			continue
		}
		for _, module := range p.Modules {
			add(p.Environment, module)
		}
	}
	for _, module := range target.CoreModules {
		add(serverpkg.EnvironmentServer, module)
	}
	return identities
}

// moduleKey scopes a module identity so identically named modules in
// different environments or module namespaces stay distinct.
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
	runtime := runtimeModules(target)
	clientOnly := clientOnlyIdentities(packages, target)
	index, err := buildModuleIndex(packages, target, runtime)
	if err != nil {
		return nil, err
	}
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
				target.moduleNamespace(m.Ecosystem),
				normalizeModuleID(m.Ecosystem, m.ID),
			}
			if owner, ok := index[key]; !ok || !owner.source.owns(pi, mi) {
				continue
			}
			problems = append(
				problems,
				checkModuleDependencies(p, m, moduleScope{
					target:      target,
					environment: p.Environment,
					ecosystem:   m.Ecosystem,
					dialect:     m.VersionDialect,
					runtime:     runtime,
					index:       index,
					clientOnly:  clientOnly,
				})...,
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

// moduleIdentities lists every identity a module answers: its ID, the
// identities it provides, and its group-qualified ID.
func moduleIdentities(m catalog.DescriptorModule) []string {
	names := make([]string, 0, len(m.Provides)+2)
	names = append(names, m.ID)
	names = append(names, m.Provides...)
	if m.Group != "" {
		names = append(names, m.Group+":"+m.ID)
	}
	return names
}

// ModuleIdentityKeys returns every scoped identity a module answers.
func (t Target) ModuleIdentityKeys(
	environment serverpkg.Environment,
	ecosystem catalog.Ecosystem,
	m catalog.DescriptorModule,
) []ModuleIdentity {
	names := moduleIdentities(m)
	identities := make([]ModuleIdentity, 0, len(names))
	namespace := t.moduleNamespace(ecosystem)
	for _, name := range names {
		identities = append(identities, ModuleIdentity{
			Environment: environment,
			Namespace:   namespace,
			ID:          normalizeModuleID(ecosystem, name),
		})
	}
	return identities
}

// ModuleOrder compares two modules that answer one identity. A native module
// outranks a nested one; otherwise the higher version wins. It returns 0 when
// the versions are equal or unparseable.
func ModuleOrder(a, b catalog.DescriptorModule) int {
	aNative := a.NestedPath == ""
	bNative := b.NestedPath == ""
	if aNative != bNative {
		if aNative {
			return 1
		}
		return -1
	}
	aVersion, err := versionpkg.ParseVersion(a.Version, a.VersionDialect)
	if err != nil {
		return 0
	}
	bVersion, err := versionpkg.ParseVersion(b.Version, b.VersionDialect)
	if err != nil {
		return 0
	}
	comparison, comparable := versionpkg.Compare(aVersion, bVersion)
	if !comparable {
		return 0
	}
	return comparison
}

// buildModuleIndex resolves every module identity to the declaration that
// answers it. A native module outranks a bundled copy of the same identity.
// Bundled copies from different packages compare versions; bundled copies from
// the same package are loader-selected variants, so the one whose requirements
// the server satisfies wins first. Two native modules in different packages are
// ambiguous.
func buildModuleIndex(
	packages []document.Package,
	target Target,
	runtime map[string]catalog.DescriptorModule,
) (map[moduleKey]moduleProvider, error) {
	index := map[moduleKey]moduleProvider{}
	for pi := range packages {
		p := &packages[pi]
		if !p.Enabled() {
			continue
		}
		for mi, m := range p.Modules {
			for _, name := range moduleIdentities(m) {
				module := m
				if name != m.ID {
					module.Version = m.ProvidedVersion(name)
				}
				candidate := moduleProvider{
					source: moduleSource{
						packageIndex: pi,
						moduleIndex:  mi,
					},
					module:    module,
					provider:  p.Provider,
					projectID: p.ProjectID,
				}
				key := moduleKey{
					p.Environment,
					target.moduleNamespace(m.Ecosystem),
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
					target,
					runtime,
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
	target Target,
	runtime map[string]catalog.DescriptorModule,
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
	if previous.source.packageIndex == candidate.source.packageIndex {
		previousServes, previousKnown := servesTarget(previous.module, target, runtime)
		candidateServes, candidateKnown := servesTarget(candidate.module, target, runtime)
		if previousKnown && candidateKnown && previousServes != candidateServes {
			return previousServes, nil
		}
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

// servesTarget reports whether the requirements a module places on the modules
// the server itself provides hold for target. A wrapper that bundles one nested
// jar per Minecraft line publishes every variant under one identity with
// disjoint requirements, and the loader loads the variant the running version
// satisfies. Requirements that another package answers are not part of that
// decision, because they do not vary with the server.
//
// The second result is false when a requirement cannot be evaluated, so the
// caller keeps its version order instead of guessing.
func servesTarget(
	m catalog.DescriptorModule,
	target Target,
	runtime map[string]catalog.DescriptorModule,
) (bool, bool) {
	scope := moduleScope{
		target:    target,
		ecosystem: m.Ecosystem,
		dialect:   m.VersionDialect,
		runtime:   runtime,
	}
	for di := range m.Dependencies {
		dep := &m.Dependencies[di]
		if dep.Kind != catalog.DependencyKindRequired || dep.ID == "java" {
			continue
		}
		outcome, err := scope.evaluate(dep, false)
		if err != nil {
			return false, false
		}
		if outcome.present && !outcome.matched {
			return false, true
		}
	}
	return true, true
}

// runtimeModules reports the modules the runtime itself provides. They answer
// requirements before any package does, the same way a loader resolves its own
// modules before the package graph.
func runtimeModules(target Target) map[string]catalog.DescriptorModule {
	modules := map[string]catalog.DescriptorModule{}
	add := func(module catalog.DescriptorModule) {
		id := normalizeModuleID(module.Ecosystem, module.ID)
		if id != "" {
			if _, ok := modules[id]; !ok {
				modules[id] = module
			}
		}
		if module.Group != "" && id != "" {
			groupID := module.Group + ":" + id
			if _, ok := modules[groupID]; !ok {
				alias := module
				alias.ID = groupID
				alias.Provides = nil
				alias.ProvidedVersions = nil
				modules[groupID] = alias
			}
		}
		for _, name := range module.Provides {
			key := normalizeModuleID(module.Ecosystem, name)
			if key == "" {
				continue
			}
			if _, ok := modules[key]; ok {
				continue
			}
			alias := module
			alias.ID = name
			alias.Version = module.ProvidedVersion(name)
			alias.Group = ""
			alias.Provides = nil
			alias.ProvidedVersions = nil
			alias.Dependencies = nil
			modules[key] = alias
		}
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
	case serverpkg.ProductQuilt:
		add(catalog.DescriptorModule{
			ID:        "quilt_loader",
			Version:   target.CoreVersion,
			Ecosystem: catalog.EcosystemQuilt,
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
