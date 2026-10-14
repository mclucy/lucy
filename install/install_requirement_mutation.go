package install

import (
	"fmt"
	"maps"
	"slices"

	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/types"
)

// declaredRequirements collects every requirement the manifest declares for a
// package, keyed by the reference that names it.
func declaredRequirements(d *manifest.Document) map[string]manifest.Requirement {
	declared := map[string]manifest.Requirement{}
	loaders := []types.Ecosystem{}
	for loader := range d.Server.Packages {
		loaders = append(loaders, loader)
	}
	slices.Sort(loaders)
	for _, loader := range loaders {
		refs := []string{}
		for ref := range d.Server.Packages[loader] {
			refs = append(refs, ref)
		}
		slices.Sort(refs)
		for _, ref := range refs {
			declared[ref] = d.Server.Packages[loader][ref]
		}
	}
	if d.MCDR != nil {
		refs := slices.Sorted(maps.Keys(d.MCDR.Packages))
		for _, ref := range refs {
			declared[ref] = d.MCDR.Packages[ref]
		}
	}
	return declared
}

// lockPackageFor reports whether a reference names one locked package.
func lockPackageFor(ref string, p types.LockedPackage) bool {
	provider, name, err := manifest.ParseReference(ref)
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
func bindRequirement(p *types.LockedPackage, ref string, requirement manifest.Requirement) {
	binding := types.RequirementBinding{
		Reference: ref,
		Selector:  requirement.Selector(),
		File:      requirement.File,
		Enabled:   requirement.IsEnabled(),
	}
	for i := range p.Requirements {
		if p.Requirements[i].Reference != ref {
			continue
		}
		p.Requirements[i] = binding
		return
	}
	if binding.Enabled {
		p.Requirements = append(p.Requirements, binding)
	}
}

// rebindRequirements reapplies the manifest's requirements onto a lock that
// already resolved them. A locked mutation never reaches upstream, so it can
// only enable or disable what the lock already pins; a declared requirement the
// lock does not satisfy is reported rather than resolved. Packages left bound
// only to requirements the manifest no longer declares are dropped, so a locked
// removal uninstalls the package instead of leaving the lock pinning it.
func rebindRequirements(d *manifest.Document, resolved *lockfile.Document) error {
	declared := declaredRequirements(d)
	for _, ref := range slices.Sorted(maps.Keys(declared)) {
		requirement := declared[ref]
		pinned := false
		for i := range resolved.Packages {
			p := &resolved.Packages[i]
			if !lockPackageFor(ref, *p) {
				continue
			}
			pinned = true
			bindRequirement(p, ref, requirement)
		}
		if !pinned && requirement.IsEnabled() {
			return fmt.Errorf(
				"lucy-lock.yaml does not pin %s; install it without --locked first",
				ref,
			)
		}
	}
	resolved.Packages = retainDeclared(resolved.Packages, declared)
	return nil
}

// retainDeclared keeps the packages a locked mutation still justifies. A
// package bound only to requirements the manifest no longer declares is
// dropped; packages with no bindings of their own are ambient and survive.
func retainDeclared(
	packages []types.LockedPackage,
	declared map[string]manifest.Requirement,
) []types.LockedPackage {
	retained := make([]types.LockedPackage, 0, len(packages))
	for _, p := range packages {
		if len(p.Requirements) == 0 {
			retained = append(retained, p)
			continue
		}
		bindings := make([]types.RequirementBinding, 0, len(p.Requirements))
		for _, binding := range p.Requirements {
			if _, ok := declared[binding.Reference]; ok {
				bindings = append(bindings, binding)
			}
		}
		if len(bindings) == 0 {
			continue
		}
		p.Requirements = bindings
		retained = append(retained, p)
	}
	return retained
}

// mutateLock produces the lock a locked mutation commits. Only requirements and
// the manifest fingerprint move; every artifact pin is copied as recorded.
func mutateLock(d *manifest.Document, old *lockfile.Document, fingerprint string) (*lockfile.Document, error) {
	resolved := *old
	resolved.ManifestFingerprint = fingerprint
	resolved.Files = slices.Clone(d.Files)
	resolved.Packages = make([]types.LockedPackage, len(old.Packages))
	for i, p := range old.Packages {
		resolved.Packages[i] = p
		resolved.Packages[i].Requirements = slices.Clone(p.Requirements)
	}
	if err := rebindRequirements(d, &resolved); err != nil {
		return nil, err
	}
	return &resolved, nil
}
