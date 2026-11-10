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
// server, so requirements they alone would answer are not failures.
func clientOnlyIdentities(
	packages []document.Package,
	target Target,
) map[string]struct{} {
	identities := map[string]struct{}{}
	add := func(ecosystem catalog.Ecosystem, names []string) {
		for _, name := range names {
			key := normalizeModuleID(ecosystem, name)
			if key != "" {
				identities[key] = struct{}{}
			}
		}
	}
	for pi := range packages {
		p := &packages[pi]
		if !p.Enabled() {
			continue
		}
		for _, module := range p.Modules {
			add(module.Ecosystem, module.ClientOnly)
		}
	}
	for _, module := range target.CoreModules {
		add(module.Ecosystem, module.ClientOnly)
	}
	return identities
}

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

// moduleEcosystem collapses the Quilt and Fabric module namespaces under a
// Quilt target. Quilt Loader resolves both descriptor kinds in one mod graph,
// so a Quilt mod can depend on a Fabric module and the reverse is impossible:
// a Fabric target never normalizes the other way.
func moduleEcosystem(
	target Target,
	ecosystem catalog.Ecosystem,
) catalog.Ecosystem {
	if target.Product != serverpkg.ProductQuilt {
		return ecosystem
	}
	if ecosystem == catalog.EcosystemQuilt ||
		ecosystem == catalog.EcosystemFabric {
		return catalog.EcosystemFabric
	}
	return ecosystem
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
				moduleEcosystem(target, m.Ecosystem),
				normalizeModuleID(m.Ecosystem, m.ID),
			}
			if owner, ok := index[key]; !ok || !owner.source.owns(pi, mi) {
				continue
			}
			problems = append(
				problems,
				checkModuleDependencies(
					p,
					m,
					target,
					index,
					runtime,
					clientOnly,
				)...,
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
			names := make([]string, 0, len(m.Provides)+2)
			names = append(names, m.ID)
			names = append(names, m.Provides...)
			if m.Group != "" {
				names = append(names, m.Group+":"+m.ID)
			}
			for _, name := range names {
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
					moduleEcosystem(target, m.Ecosystem),
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
	for di := range m.Dependencies {
		dep := &m.Dependencies[di]
		if dep.Kind != catalog.DependencyKindRequired || dep.ID == "java" {
			continue
		}
		exempt, known := runtimeUnlessMatches(
			dep,
			m.Ecosystem,
			m.VersionDialect,
			runtime,
		)
		if !known {
			return false, false
		}
		if exempt {
			continue
		}
		matched, known := runtimeRequirementMatches(
			dep,
			m.Ecosystem,
			target,
			m.VersionDialect,
			runtime,
		)
		if !known {
			return false, false
		}
		if !matched {
			return false, true
		}
	}
	return true, true
}

// runtimeUnlessMatches reports whether any unless condition of dep holds for
// the modules the server provides. A condition that names a module the runtime
// does not provide cannot be evaluated here.
func runtimeUnlessMatches(
	dep *catalog.DescriptorDependency,
	ecosystem catalog.Ecosystem,
	dialect versionpkg.Dialect,
	runtime map[string]catalog.DescriptorModule,
) (bool, bool) {
	for i := range dep.Unless {
		condition := &dep.Unless[i]
		provided, ok := runtime[normalizeModuleID(ecosystem, condition.ID)]
		if !ok {
			continue
		}
		matched := condition.Constraint == ""
		if !matched {
			var err error
			matched, err = versionpkg.Match(
				condition.Constraint,
				provided.Version,
				dialect,
			)
			if err != nil {
				return false, false
			}
		}
		if matched {
			return true, true
		}
	}
	return false, true
}

// runtimeRequirementMatches checks a dependency and its alternatives against
// the modules the runtime provides. A requirement answered by a package is
// deliberately left out of the variant decision.
func runtimeRequirementMatches(
	dep *catalog.DescriptorDependency,
	ecosystem catalog.Ecosystem,
	target Target,
	dialect versionpkg.Dialect,
	runtime map[string]catalog.DescriptorModule,
) (bool, bool) {
	alternatives := make(
		[]*catalog.DescriptorDependency,
		0,
		1+len(dep.AnyOf),
	)
	alternatives = append(alternatives, dep)
	for i := range dep.AnyOf {
		alternatives = append(alternatives, &dep.AnyOf[i])
	}
	providedAny := false
	for _, alternative := range alternatives {
		provided, ok := runtime[normalizeModuleID(ecosystem, alternative.ID)]
		if !ok {
			continue
		}
		providedAny = true
		matched := fmlLoader(target) && provided.Version == "0.0NONE"
		if !matched && alternative.Constraint == "" {
			matched = true
		}
		if !matched && alternative.Constraint != "" {
			var err error
			matched, err = versionpkg.Match(
				alternative.Constraint,
				provided.Version,
				dialect,
			)
			if err != nil {
				return false, false
			}
		}
		if !matched && alternative.Constraint != "" {
			var err error
			matched, err = matchesLoaderCompatibility(
				alternative.Constraint,
				alternative.ID,
				target,
				dialect,
			)
			if err != nil {
				return false, false
			}
		}
		if matched {
			return true, true
		}
	}
	// A requirement no runtime module answers is settled by the package graph,
	// not by this variant decision.
	return !providedAny, true
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

func checkModuleDependencies(
	p *document.Package,
	m catalog.DescriptorModule,
	target Target,
	index map[moduleKey]moduleProvider,
	runtime map[string]catalog.DescriptorModule,
	clientOnly map[string]struct{},
) []ModuleProblem {
	var problems []ModuleProblem
	for di := range m.Dependencies {
		dep := &m.Dependencies[di]
		// A Java requirement is checked against the Java runtime that runs
		// the server, and only warns.
		if dep.Kind == catalog.DependencyKindPython || dep.ID == "java" {
			continue
		}
		exempt, err := moduleDependencyExempted(
			dep,
			p.Environment,
			m.Ecosystem,
			target,
			m.VersionDialect,
			index,
			runtime,
		)
		if err != nil {
			problems = append(problems, moduleProblem(
				p,
				m.ID,
				*dep,
				"",
				false,
				fmt.Sprintf("module %s requires %s: %v", m.ID, dep.ID, err),
			))
			continue
		}
		if exempt {
			continue
		}
		outcome, err := matchModuleDependency(
			dep,
			p.Environment,
			m.Ecosystem,
			target,
			m.VersionDialect,
			index,
			runtime,
			clientOnly,
		)
		if err != nil {
			problems = append(problems, moduleProblem(
				p,
				m.ID,
				*dep,
				outcome.value,
				false,
				fmt.Sprintf("module %s requires %s: %v", m.ID, dep.ID, err),
			))
			continue
		}

		requirement := dependencyRequirementText(dep)
		serverProvided := outcome.owner == nil && outcome.value != ""
		switch {
		case dep.Kind == catalog.DependencyKindIncompatible:
			if outcome.matched {
				problems = append(problems, moduleProblem(
					p,
					m.ID,
					*dep,
					outcome.value,
					serverProvided,
					fmt.Sprintf(
						"module %s breaks %s %s",
						m.ID,
						dependencyAlternativeID(dep, outcome.matchedID),
						outcome.value,
					),
				))
			}
		case len(dep.AnyOf) > 0 &&
			(dep.Kind == catalog.DependencyKindRequired ||
				dep.Kind == catalog.DependencyKindOptional):
			if !outcome.matched {
				if outcome.value == "" {
					problems = append(problems, moduleProblem(
						p,
						m.ID,
						*dep,
						"",
						false,
						fmt.Sprintf(
							"module %s requires %s, which no selected package provides",
							m.ID,
							requirement,
						),
					))
				} else {
					problems = append(problems, moduleProblem(
						p,
						m.ID,
						*dep,
						outcome.value,
						serverProvided,
						fmt.Sprintf(
							"module %s requires %s (selected %s)",
							m.ID,
							requirement,
							outcome.value,
						),
					))
				}
			}
		case dep.Kind == catalog.DependencyKindOptional:
			// An optional dependency is checked only when it is present.
			if outcome.present && !outcome.matched {
				problems = append(problems, moduleProblem(
					p,
					m.ID,
					*dep,
					outcome.value,
					serverProvided,
					fmt.Sprintf(
						"module %s optionally requires %s (selected %s)",
						m.ID,
						requirement,
						outcome.value,
					),
				))
			}
		case dep.Kind == catalog.DependencyKindRequired:
			if !outcome.matched {
				if outcome.value == "" {
					problems = append(problems, moduleProblem(
						p,
						m.ID,
						*dep,
						"",
						false,
						fmt.Sprintf(
							"module %s requires %s, which no selected package provides",
							m.ID,
							requirement,
						),
					))
				} else {
					problems = append(problems, moduleProblem(
						p,
						m.ID,
						*dep,
						outcome.value,
						serverProvided,
						fmt.Sprintf(
							"module %s requires %s (selected %s)",
							m.ID,
							requirement,
							outcome.value,
						),
					))
				}
			}
		}

		if outcome.matched && !outcome.waived {
			if outcome.owner != nil && !outcome.owner.source.runtime() {
				dep.Provider = outcome.owner.provider
				dep.ProjectID = outcome.owner.projectID
				dep.ModuleID = outcome.owner.module.ID
			} else if outcome.matchedID != "" {
				dep.ModuleID = outcome.matchedID
			} else {
				dep.ModuleID = dep.ID
			}
		}
	}
	return problems
}

// moduleRequirement records the selected module state a dependency and its
// alternatives observe.
type moduleRequirement struct {
	present   bool
	matched   bool
	waived    bool
	value     string
	owner     *moduleProvider
	matchedID string
}

// moduleDependencyExempted reports whether an unless condition of dep holds.
// Any matching condition makes the dependency unnecessary.
func moduleDependencyExempted(
	dep *catalog.DescriptorDependency,
	environment serverpkg.Environment,
	ecosystem catalog.Ecosystem,
	target Target,
	dialect versionpkg.Dialect,
	index map[moduleKey]moduleProvider,
	runtime map[string]catalog.DescriptorModule,
) (bool, error) {
	for i := range dep.Unless {
		matched, err := moduleConditionMatches(
			&dep.Unless[i],
			environment,
			ecosystem,
			target,
			dialect,
			index,
			runtime,
		)
		if err != nil {
			return false, err
		}
		if matched {
			return true, nil
		}
	}
	return false, nil
}

func moduleConditionMatches(
	condition *catalog.DescriptorDependency,
	environment serverpkg.Environment,
	ecosystem catalog.Ecosystem,
	target Target,
	dialect versionpkg.Dialect,
	index map[moduleKey]moduleProvider,
	runtime map[string]catalog.DescriptorModule,
) (bool, error) {
	exempt, err := moduleDependencyExempted(
		condition,
		environment,
		ecosystem,
		target,
		dialect,
		index,
		runtime,
	)
	if err != nil || exempt {
		return false, err
	}
	value, _ := lookupSelectedModule(
		target,
		environment,
		ecosystem,
		condition.ID,
		index,
		runtime,
	)
	if value == "" {
		return false, nil
	}
	return matchModuleConstraint(condition, value, target, dialect)
}

// matchModuleDependency evaluates a dependency together with its any-of
// alternatives against the selected modules.
func matchModuleDependency(
	dep *catalog.DescriptorDependency,
	environment serverpkg.Environment,
	ecosystem catalog.Ecosystem,
	target Target,
	dialect versionpkg.Dialect,
	index map[moduleKey]moduleProvider,
	runtime map[string]catalog.DescriptorModule,
	clientOnly map[string]struct{},
) (moduleRequirement, error) {
	alternatives := make(
		[]*catalog.DescriptorDependency,
		0,
		1+len(dep.AnyOf),
	)
	alternatives = append(alternatives, dep)
	for i := range dep.AnyOf {
		alternatives = append(alternatives, &dep.AnyOf[i])
	}

	var outcome moduleRequirement
	group := len(dep.AnyOf) > 0
	for _, alternative := range alternatives {
		exempt, err := moduleDependencyExempted(
			alternative,
			environment,
			ecosystem,
			target,
			dialect,
			index,
			runtime,
		)
		if err != nil {
			return outcome, err
		}
		if exempt {
			if dep.Kind == catalog.DependencyKindIncompatible {
				// An exempt break does not apply.
				continue
			}
			// An unless condition satisfies a requirement alternative.
			outcome.matched = true
			outcome.waived = true
			return outcome, nil
		}
		value, owner := lookupSelectedModule(
			target,
			environment,
			ecosystem,
			alternative.ID,
			index,
			runtime,
		)
		if value != "" {
			outcome.present = true
			if outcome.value == "" {
				outcome.value = value
			}
		}
		var matched bool
		if value != "" {
			matched, err = matchModuleConstraint(
				alternative,
				value,
				target,
				dialect,
			)
		} else if alternative.Constraint != "" {
			matched, err = matchesLoaderCompatibility(
				alternative.Constraint,
				alternative.ID,
				target,
				dialect,
			)
		}
		if err != nil {
			return outcome, err
		}
		if matched {
			outcome.matched = true
			outcome.value = value
			outcome.owner = owner
			outcome.matchedID = alternative.ID
			break
		}
		if owner == nil && value == "" &&
			dep.Kind != catalog.DependencyKindIncompatible {
			if _, ok := clientOnly[normalizeModuleID(
				ecosystem,
				alternative.ID,
			)]; ok {
				// The only candidate is client-only, so the loader filters it
				// out of a dedicated server instead of failing the dependency.
				outcome.matched = true
				outcome.waived = true
				return outcome, nil
			}
		}
		if group &&
			alternative.Kind == catalog.DependencyKindOptional &&
			value == "" {
			// An absent optional alternative satisfies an any-of group.
			outcome.matched = true
			outcome.waived = true
			return outcome, nil
		}
	}
	if !outcome.matched {
		return outcome, nil
	}
	// A conjunctive entry, such as a Quilt breaks array, applies only when
	// every one of its conditions matches.
	for i := range dep.AllOf {
		condition, err := matchModuleDependency(
			&dep.AllOf[i],
			environment,
			ecosystem,
			target,
			dialect,
			index,
			runtime,
			clientOnly,
		)
		if err != nil {
			return outcome, err
		}
		if !condition.matched {
			outcome.matched = false
			return outcome, nil
		}
	}
	return outcome, nil
}

func lookupSelectedModule(
	target Target,
	environment serverpkg.Environment,
	ecosystem catalog.Ecosystem,
	id string,
	index map[moduleKey]moduleProvider,
	runtime map[string]catalog.DescriptorModule,
) (string, *moduleProvider) {
	if module, ok := runtime[normalizeModuleID(ecosystem, id)]; ok {
		return string(module.Version), nil
	}
	key := moduleKey{
		environment,
		moduleEcosystem(target, ecosystem),
		normalizeModuleID(ecosystem, id),
	}
	if provider, ok := index[key]; ok {
		return string(provider.module.Version), &provider
	}
	return "", nil
}

// matchModuleConstraint applies one dependency constraint to a selected
// version, including the loader support matrices that substitute versions for
// modules the loader knows about.
func matchModuleConstraint(
	dep *catalog.DescriptorDependency,
	value string,
	target Target,
	dialect versionpkg.Dialect,
) (bool, error) {
	if fmlLoader(target) && value == "0.0NONE" {
		// FMLLoader treats the 0.0NONE sentinel as satisfying every range.
		return true, nil
	}
	if dep.Constraint == "" {
		return true, nil
	}
	matched, err := versionpkg.Match(
		dep.Constraint,
		versionpkg.Version(value),
		dialect,
	)
	if err != nil {
		return false, err
	}
	if matched {
		return true, nil
	}
	return matchesLoaderCompatibility(dep.Constraint, dep.ID, target, dialect)
}

func dependencyRequirementText(dep *catalog.DescriptorDependency) string {
	parts := make([]string, 0, 1+len(dep.AnyOf))
	parts = append(parts, strings.TrimSpace(dep.ID+" "+dep.Constraint))
	for i := range dep.AnyOf {
		alternative := &dep.AnyOf[i]
		parts = append(
			parts,
			strings.TrimSpace(alternative.ID+" "+alternative.Constraint),
		)
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "one of " + strings.Join(parts, ", ")
}

func dependencyAlternativeID(
	dep *catalog.DescriptorDependency,
	matched string,
) string {
	if matched != "" {
		return matched
	}
	return dep.ID
}
