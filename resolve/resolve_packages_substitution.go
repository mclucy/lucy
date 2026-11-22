package resolve

import (
	"cmp"
	"iter"
	"maps"
	"slices"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"

	versionpkg "github.com/mclucy/lucy/version"
)

// A runtime that loads several descriptor ecosystems resolves all their
// modules in one namespace. When a package of a preferred descriptor
// ecosystem answers every module identity of a package of a later one, such
// as Quilted Fabric API answering Fabric API on a Quilt server, the loader
// would see two native modules with one identity. The search therefore binds
// a pure dependency demand to the preferred package instead of installing
// both. Direct requirements and exact release demands are never substituted.

// substitute returns the selected package that can stand in for dependency.
// Among several, the first in key order wins.
func (s *selectionState) substitute(
	target Target,
	dependency document.Package,
) (projectKey, bool) {
	if rank, ok := target.descriptorRank(dependency); !ok || rank == 0 {
		return projectKey{}, false
	}
	for _, key := range s.keys() {
		provider := s.chosen[key]
		if provider.Artifact.Filename == "" {
			continue
		}
		if providesFor(target, provider, dependency, maps.Values(s.chosen)) {
			return key, true
		}
	}
	return projectKey{}, false
}

// Upstream dependency edges name projects, not descriptor ecosystems. For
// example, a Quilt mod can depend on a Fabric project already loaded by Quilt.
// Direct manifest bindings keep their own ecosystem.
func (s *selectionState) selectedRuntimeProject(target Target, key projectKey) projectKey {
	if p, found := s.chosen[key]; found && p.Filename != "" {
		return key
	}
	runtime, ok := target.PackageRuntime()
	if !ok {
		return key
	}
	for _, ecosystem := range runtime.Loads() {
		if ecosystem == key.ecosystem || target.moduleNamespace(ecosystem) != target.moduleNamespace(key.ecosystem) {
			continue
		}
		other := key
		other.ecosystem = ecosystem
		if p, found := s.chosen[other]; found && p.Filename != "" {
			return other
		}
	}
	return key
}

// supersede removes every selected pure dependency package that the package at
// key stands in for, and moves their dependents onto it. It returns the
// branches that undo the change.
func (s *selectionState) supersede(
	target Target,
	key projectKey,
) []selectionBranch {
	provider := s.chosen[key]
	rank, ok := target.descriptorRank(provider)
	runtime, _ := target.PackageRuntime()
	if !ok || rank+1 == len(runtime.Loads()) {
		return nil
	}
	var branches []selectionBranch
	var dependents []catalog.Ref
	for _, other := range s.keys() {
		dependency := s.chosen[other]
		if other == key ||
			!dependency.Dependency ||
			len(dependency.Requirements) > 0 ||
			s.pinned[other] ||
			dependency.Artifact.Filename == "" {
			continue
		}
		if !providesFor(target, provider, dependency, maps.Values(s.chosen)) {
			continue
		}
		branches = append(branches, s.remove(other))
		dependents = append(dependents, dependency.DependencyOf...)
	}
	if len(branches) == 0 {
		return nil
	}
	return append(
		branches,
		s.extend(key, withDependents(provider, dependents), false),
	)
}

func (s *selectionState) keys() []projectKey {
	keys := make([]projectKey, 0, len(s.chosen))
	for key := range s.chosen {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b projectKey) int {
		return cmp.Or(
			strings.Compare(string(a.environment), string(b.environment)),
			strings.Compare(string(a.ecosystem), string(b.ecosystem)),
			strings.Compare(string(a.provider), string(b.provider)),
			strings.Compare(a.project, b.project),
		)
	})
	return keys
}

// withDependents records dependents as packages that depend on p.
func withDependents(p document.Package, dependents []catalog.Ref) document.Package {
	if len(dependents) == 0 {
		return p
	}
	p.Dependency = true
	p.DependencyOf = slices.Clone(p.DependencyOf)
	for _, dependent := range dependents {
		if !slices.Contains(p.DependencyOf, dependent) {
			p.DependencyOf = append(p.DependencyOf, dependent)
		}
	}
	return p
}

// providesFor reports whether provider can stand in for dependency on
// target. The provider must share the dependency's environment, come before
// it in the runtime's descriptor precedence, answer every module identity of
// the dependency, and expose versions that the constraints of the selected
// graph accept.
func providesFor(
	target Target,
	provider document.Package,
	dependency document.Package,
	selected iter.Seq[document.Package],
) bool {
	if provider.Environment != dependency.Environment ||
		samePackage(provider, dependency) {
		return false
	}
	providerRank, ok := target.descriptorRank(provider)
	if !ok {
		return false
	}
	dependencyRank, ok := target.descriptorRank(dependency)
	if !ok || providerRank >= dependencyRank {
		return false
	}
	return coversModules(target, provider, dependency, selected)
}

func coversModules(
	target Target,
	provider, dependency document.Package,
	selected iter.Seq[document.Package],
) bool {
	provided := identityVersions(target, provider)
	required := identityVersions(target, dependency)
	if len(required) == 0 {
		return false
	}
	for key := range required {
		if _, ok := provided[key]; !ok {
			return false
		}
	}
	for p := range selected {
		if samePackage(p, dependency) {
			continue
		}
		if !acceptsVersions(target, p, provided) {
			return false
		}
	}
	return true
}

func samePackage(a, b document.Package) bool {
	return a.Environment == b.Environment &&
		a.Ecosystem == b.Ecosystem &&
		a.Provider == b.Provider &&
		a.ProjectID == b.ProjectID
}

// descriptorRank returns the position of a package's descriptor ecosystem in
// the precedence of the target runtime. Native modules decide it; a package
// without one is ranked by its bundled modules.
func (t Target) descriptorRank(p document.Package) (int, bool) {
	runtime, ok := t.PackageRuntime()
	if !ok {
		return 0, false
	}
	loads := runtime.Loads()
	rank := func(native bool) int {
		best := len(loads)
		for _, m := range p.Modules {
			if native && m.NestedPath != "" {
				continue
			}
			if i := slices.Index(loads, m.Ecosystem); i >= 0 && i < best {
				best = i
			}
		}
		return best
	}
	best := rank(true)
	if best == len(loads) {
		best = rank(false)
	}
	return best, best < len(loads)
}

// identityVersions maps every module identity of p, scoped like module
// resolution, to the version it exposes. The first declaration of an
// identity wins.
func identityVersions(
	target Target,
	p document.Package,
) map[moduleKey]versionpkg.Version {
	versions := map[moduleKey]versionpkg.Version{}
	for _, m := range p.Modules {
		for _, name := range moduleIdentities(m) {
			if name == "" {
				continue
			}
			key := moduleKey{
				p.Environment,
				target.moduleNamespace(m.Ecosystem),
				normalizeModuleID(m.Ecosystem, name),
			}
			if _, exists := versions[key]; !exists {
				versions[key] = m.ProvidedVersion(name)
			}
		}
	}
	return versions
}

// acceptsVersions reports whether the plain version constraints that the
// modules of p place on provided identities accept the provided versions.
// Required and optional constraints must match, and a break must fail the
// match. Compound nodes are left to the final module check, which evaluates
// complete dependency trees.
func acceptsVersions(
	target Target,
	p document.Package,
	provided map[moduleKey]versionpkg.Version,
) bool {
	for _, m := range p.Modules {
		for _, dep := range m.Dependencies {
			if dep.Constraint == "" ||
				len(dep.AnyOf) > 0 ||
				len(dep.AllOf) > 0 ||
				len(dep.Unless) > 0 {
				continue
			}
			version, ok := provided[moduleKey{
				p.Environment,
				target.moduleNamespace(m.Ecosystem),
				normalizeModuleID(m.Ecosystem, dep.ID),
			}]
			if !ok {
				continue
			}
			matched, err := versionpkg.Match(dep.Constraint, version, m.VersionDialect)
			if err != nil {
				return false
			}
			switch dep.Kind {
			case catalog.DependencyKindRequired, catalog.DependencyKindOptional:
				if !matched {
					return false
				}
			case catalog.DependencyKindIncompatible:
				if matched {
					return false
				}
			}
		}
	}
	return true
}
