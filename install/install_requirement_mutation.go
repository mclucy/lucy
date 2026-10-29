package install

import (
	"cmp"
	"fmt"
	"maps"
	"slices"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	serverpkg "github.com/mclucy/lucy/server"
)

// requirementKey names one manifest declaration. A reference alone does not
// identify a declaration because the same project can be declared for two
// ecosystems or environments.
type requirementKey struct {
	environment serverpkg.Environment
	ecosystem   catalog.Ecosystem
	reference   string
}

func compareRequirementKeys(a, b requirementKey) int {
	return cmp.Or(
		cmp.Compare(a.environment, b.environment),
		cmp.Compare(a.ecosystem, b.ecosystem),
		cmp.Compare(a.reference, b.reference),
	)
}

// declaredRequirements collects every requirement the manifest declares for a
// package, keyed by the location and reference that name it.
func declaredRequirements(d *document.Manifest) map[requirementKey]document.Requirement {
	declared := map[requirementKey]document.Requirement{}
	var loaders []catalog.Ecosystem
	for loader := range d.Server.Packages {
		loaders = append(loaders, loader)
	}
	slices.Sort(loaders)
	for _, loader := range loaders {
		var refs []string
		for ref := range d.Server.Packages[loader] {
			refs = append(refs, ref)
		}
		slices.Sort(refs)
		for _, ref := range refs {
			key := requirementKey{serverpkg.EnvironmentServer, loader, ref}
			declared[key] = d.Server.Packages[loader][ref]
		}
	}
	if d.MCDR != nil {
		refs := slices.Sorted(maps.Keys(d.MCDR.Packages))
		for _, ref := range refs {
			key := requirementKey{serverpkg.EnvironmentMCDR, catalog.EcosystemMCDR, ref}
			declared[key] = d.MCDR.Packages[ref]
		}
	}
	return declared
}

// lockPackageFor reports whether a reference names one locked package.
func lockPackageFor(ref string, p document.Package) bool {
	provider, name, err := document.ParseReference(ref)
	if err != nil {
		return false
	}
	if p.Provider != provider {
		return false
	}
	if p.ProjectID == name {
		return true
	}
	for _, module := range p.Modules {
		if module.ID == name || slices.Contains(module.Provides, name) {
			return true
		}
	}
	return false
}

// bindRequirement records the manifest's current state of one requirement on
// the package that satisfies it. A requirement the lock has never seen is
// added, so a package resolved only as a dependency becomes declared.
func bindRequirement(p *document.Package, ref string, requirement document.Requirement) {
	binding := document.RequirementBinding{
		Reference:   ref,
		Selector:    requirement.Selector,
		FilePattern: requirement.FilePattern,
		Enabled:     requirement.IsEnabled(),
	}
	for i := range p.Requirements {
		if p.Requirements[i].Reference != ref {
			continue
		}
		p.Requirements[i] = binding
		return
	}
	if binding.Enabled || len(p.DependencyOf) > 0 {
		p.Requirements = append(p.Requirements, binding)
	}
}

// rebindRequirements reapplies the manifest's requirements onto a lock that
// already resolved them. A locked mutation never reaches upstream, so it can
// only enable or disable what the lock already pins; a declared requirement the
// lock does not satisfy is reported rather than resolved. Packages left bound
// only to requirements the manifest no longer declares are dropped, so a locked
// removal uninstalls the package instead of leaving the lock pinning it.
func rebindRequirements(d *document.Manifest, resolved *document.Lock) error {
	declared := declaredRequirements(d)
	keys := make([]requirementKey, 0, len(declared))
	for key := range declared {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, compareRequirementKeys)
	for _, key := range keys {
		requirement := declared[key]
		pinned := false
		for i := range resolved.Packages {
			p := &resolved.Packages[i]
			if p.Environment != key.environment || p.Ecosystem != key.ecosystem {
				continue
			}
			if !lockPackageFor(key.reference, *p) {
				continue
			}
			pinned = true
			bindRequirement(p, key.reference, requirement)
		}
		if !pinned && requirement.IsEnabled() {
			return fmt.Errorf(
				"lucy-lock.yaml does not pin %s; install it without --locked first",
				key.reference,
			)
		}
	}
	resolved.Packages = retainDeclared(resolved.Packages, declared)
	return nil
}

// retainDeclared keeps pins reachable from remaining declarations or ambient
// packages. Disabled declarations retain pins without activating their dependencies.
func retainDeclared(
	packages []document.Package,
	declared map[requirementKey]document.Requirement,
) []document.Package {
	type identity struct {
		environment string
		ecosystem   catalog.Ecosystem
		reference   catalog.Ref
	}
	identityOf := func(p *document.Package) identity {
		return identity{
			string(p.Environment),
			p.Ecosystem,
			catalog.Ref{Provider: p.Provider, Name: catalog.Name(p.ProjectID)},
		}
	}
	indices := make(map[identity]int, len(packages))
	retained := make([]bool, len(packages))
	enabled := make([]bool, len(packages))
	for i := range packages {
		p := &packages[i]
		indices[identityOf(p)] = i
		ambient := len(p.Requirements) == 0 && len(p.DependencyOf) == 0
		retained[i], enabled[i] = ambient, ambient
		bindings := p.Requirements[:0]
		for _, binding := range p.Requirements {
			key := requirementKey{p.Environment, p.Ecosystem, binding.Reference}
			if _, ok := declared[key]; ok {
				bindings = append(bindings, binding)
				retained[i] = true
				enabled[i] = enabled[i] || binding.Enabled
			}
		}
		p.Requirements = bindings
	}
	children := make([][]int, len(packages))
	for i := range packages {
		p := &packages[i]
		key := identityOf(p)
		for _, source := range p.DependencyOf {
			key.reference = source
			if parent, ok := indices[key]; ok {
				children[parent] = append(children[parent], i)
			}
		}
	}
	queue := make([]int, 0, len(packages))
	propagate := func(reached []bool) {
		queue = queue[:0]
		for i, yes := range reached {
			if yes {
				queue = append(queue, i)
			}
		}
		for next := 0; next < len(queue); next++ {
			for _, child := range children[queue[next]] {
				if !reached[child] {
					reached[child] = true
					queue = append(queue, child)
				}
			}
		}
	}
	propagate(retained)
	propagate(enabled)
	out := packages[:0]
	for i, p := range packages {
		if !retained[i] {
			continue
		}
		p.Dependency = false
		sources := p.DependencyOf[:0]
		key := identityOf(&p)
		for _, source := range p.DependencyOf {
			key.reference = source
			if parent, ok := indices[key]; ok && retained[parent] {
				sources = append(sources, source)
				p.Dependency = p.Dependency || enabled[parent]
			}
		}
		p.DependencyOf = sources
		out = append(out, p)
	}
	return out
}

// mutateLock produces the lock a locked mutation commits. Only requirements,
// the Java override, and the manifest fingerprint move; every artifact pin is
// copied as recorded.
func mutateLock(d *document.Manifest, old *document.Lock, fingerprint string) (*document.Lock, error) {
	resolved := *old
	resolved.ManifestFingerprint = fingerprint
	resolved.Files = slices.Clone(d.Files)
	resolved.Server.Java = document.NewLockedJava(old.Server.Java.Advisory, d.Server.Java)
	resolved.Packages = make([]document.Package, len(old.Packages))
	for i, p := range old.Packages {
		resolved.Packages[i] = p
		resolved.Packages[i].Requirements = slices.Clone(p.Requirements)
		resolved.Packages[i].DependencyOf = slices.Clone(p.DependencyOf)
	}
	if err := rebindRequirements(d, &resolved); err != nil {
		return nil, err
	}
	return &resolved, nil
}
