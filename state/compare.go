package state

import (
	"path/filepath"
	"slices"
	"strings"
)

type StateDiff struct {
	InManifestNotLock []string
	InLockNotManifest []string
	InLockNotObserved []string
	InObservedNotLock []string
	IgnoredObserved   []string
	UnmanagedObserved []string
}

// DiffDesiredResolved compares desired membership with resolved membership.
//
// It intentionally compares package identity only. Manifest versions may remain
// fuzzy intent selectors, while lock versions are exact facts. Exact-version
// drift for the same package ID is tracked by lock staleness
// (manifest_fingerprint mismatch) and the next resolve/install run, not by this
// membership diff.
func DiffDesiredResolved(manifest *Manifest, lock *Lock) StateDiff {
	diff := StateDiff{}

	manifestIDs := make(map[string]struct{})
	if manifest != nil {
		for _, pkg := range manifest.Packages {
			if pkg.ID == "" {
				continue
			}
			manifestIDs[pkg.ID] = struct{}{}
		}
	}

	lockIDs := make(map[string]struct{})
	if lock != nil {
		for _, pkg := range lock.Packages {
			if pkg.ID == "" {
				continue
			}
			lockIDs[pkg.ID] = struct{}{}
		}
	}

	for id := range manifestIDs {
		if _, ok := lockIDs[id]; !ok {
			diff.InManifestNotLock = append(diff.InManifestNotLock, id)
		}
	}
	for id := range lockIDs {
		if _, ok := manifestIDs[id]; !ok {
			diff.InLockNotManifest = append(diff.InLockNotManifest, id)
		}
	}

	slices.Sort(diff.InManifestNotLock)
	slices.Sort(diff.InLockNotManifest)
	return diff
}

// DiffResolvedObserved compares exact lock install targets with current
// observed paths. Observed drift is always checked against lock facts, never
// against fuzzy manifest selectors.
func DiffResolvedObserved(lock *Lock, observedPaths []string) StateDiff {
	return DiffResolvedObservedInScope(lock, observedPaths, nil)
}

// DiffResolvedObservedInScope compares exact lock install targets with observed
// paths while separating Lucy-managed drift from ignored/manual content and
// content outside managed sync scope.
func DiffResolvedObservedInScope(lock *Lock, observedPaths []string, ignoredPaths []string) StateDiff {
	diff := StateDiff{}

	lockPaths := make(map[string]struct{})
	if lock != nil {
		for _, pkg := range lock.Packages {
			normalized := normalizeRelativePath(pkg.InstallPath)
			if normalized == "" || normalized == "." {
				continue
			}
			lockPaths[normalized] = struct{}{}
		}
	}

	ignored := make(map[string]struct{}, len(ignoredPaths))
	for _, path := range ignoredPaths {
		normalized := normalizeRelativePath(path)
		if normalized == "" || normalized == "." {
			continue
		}
		ignored[normalized] = struct{}{}
	}

	observed := make(map[string]struct{}, len(observedPaths))
	for _, path := range observedPaths {
		normalized := normalizeRelativePath(path)
		if normalized == "" || normalized == "." {
			continue
		}
		observed[normalized] = struct{}{}
	}

	for path := range lockPaths {
		if _, ok := observed[path]; !ok {
			diff.InLockNotObserved = append(diff.InLockNotObserved, path)
		}
	}
	for path := range observed {
		if _, ok := lockPaths[path]; ok {
			continue
		}
		if _, ok := ignored[path]; ok {
			diff.IgnoredObserved = append(diff.IgnoredObserved, path)
			continue
		}
		diff.InObservedNotLock = append(diff.InObservedNotLock, path)
	}

	slices.Sort(diff.InLockNotObserved)
	slices.Sort(diff.InObservedNotLock)
	slices.Sort(diff.IgnoredObserved)
	slices.Sort(diff.UnmanagedObserved)
	return diff
}

// IgnoredInstallPaths resolves manifest ignored entries to their known install
// paths from the lock so observed files can stay visible without being treated
// as managed drift.
func IgnoredInstallPaths(manifest *Manifest, lock *Lock) []string {
	if manifest == nil || lock == nil {
		return nil
	}

	ignoredIDs := make(map[string]struct{})
	for _, pkg := range manifest.Packages {
		if pkg.Role != RoleIgnored || strings.TrimSpace(pkg.ID) == "" {
			continue
		}
		ignoredIDs[pkg.ID] = struct{}{}
	}

	paths := make([]string, 0, len(ignoredIDs))
	for _, pkg := range lock.Packages {
		if _, ok := ignoredIDs[pkg.ID]; !ok {
			continue
		}
		normalized := normalizeRelativePath(pkg.InstallPath)
		if normalized == "" || normalized == "." {
			continue
		}
		paths = append(paths, normalized)
	}

	slices.Sort(paths)
	return slices.Compact(paths)
}

// CompareManifestLockObserved combines intent-vs-lock and lock-vs-observed
// comparisons under Lucy's softer manifest model.
func CompareManifestLockObserved(manifest *Manifest, lock *Lock, observedPaths []string) StateDiff {
	managedManifest := manifestForComparison(manifest)
	managedLock := lockForComparison(manifest, lock)

	intentDiff := DiffDesiredResolved(managedManifest, managedLock)
	observedDiff := DiffResolvedObservedInScope(managedLock, observedPaths, IgnoredInstallPaths(manifest, lock))

	return StateDiff{
		InManifestNotLock: intentDiff.InManifestNotLock,
		InLockNotManifest: intentDiff.InLockNotManifest,
		InLockNotObserved: observedDiff.InLockNotObserved,
		InObservedNotLock: observedDiff.InObservedNotLock,
		IgnoredObserved:   observedDiff.IgnoredObserved,
		UnmanagedObserved: observedDiff.UnmanagedObserved,
	}
}

func normalizeRelativePath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

func manifestForComparison(manifest *Manifest) *Manifest {
	if manifest == nil {
		return nil
	}

	cloned := *manifest
	cloned.Packages = make([]ManifestPackage, 0, len(manifest.Packages))
	for _, pkg := range manifest.Packages {
		if pkg.Role == RoleIgnored {
			continue
		}
		cloned.Packages = append(cloned.Packages, pkg)
	}
	return &cloned
}

func lockForComparison(manifest *Manifest, lock *Lock) *Lock {
	if lock == nil {
		return nil
	}

	ignoredIDs := make(map[string]struct{})
	if manifest != nil {
		for _, pkg := range manifest.Packages {
			if pkg.Role != RoleIgnored || strings.TrimSpace(pkg.ID) == "" {
				continue
			}
			ignoredIDs[pkg.ID] = struct{}{}
		}
	}

	filtered := *lock
	filtered.Packages = make([]LockedPackage, 0, len(lock.Packages))
	for _, pkg := range lock.Packages {
		if _, ok := ignoredIDs[pkg.ID]; ok {
			continue
		}
		filtered.Packages = append(filtered.Packages, pkg)
	}
	filtered.Packages = CanonicalLockedPackages(filtered.Packages)
	return &filtered
}
