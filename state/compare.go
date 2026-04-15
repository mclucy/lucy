package state

import "slices"

type StateDiff struct {
	InManifestNotLock []string
	InLockNotManifest []string
	InLockNotObserved []string
	InObservedNotLock []string
}

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

func DiffResolvedObserved(lock *Lock, observedPaths []string) StateDiff {
	diff := StateDiff{}

	lockPaths := make(map[string]struct{})
	if lock != nil {
		for _, pkg := range lock.Packages {
			if pkg.InstallPath == "" {
				continue
			}
			lockPaths[pkg.InstallPath] = struct{}{}
		}
	}

	observed := make(map[string]struct{}, len(observedPaths))
	for _, path := range observedPaths {
		if path == "" {
			continue
		}
		observed[path] = struct{}{}
	}

	for path := range lockPaths {
		if _, ok := observed[path]; !ok {
			diff.InLockNotObserved = append(diff.InLockNotObserved, path)
		}
	}
	for path := range observed {
		if _, ok := lockPaths[path]; !ok {
			diff.InObservedNotLock = append(diff.InObservedNotLock, path)
		}
	}

	slices.Sort(diff.InLockNotObserved)
	slices.Sort(diff.InObservedNotLock)
	return diff
}

func ClassifyDrift(diff StateDiff) string {
	hasUnresolvedIntent := len(diff.InManifestNotLock) > 0 || len(diff.InLockNotManifest) > 0
	hasObservedDrift := len(diff.InLockNotObserved) > 0 || len(diff.InObservedNotLock) > 0

	switch {
	case hasUnresolvedIntent && hasObservedDrift:
		return "has both"
	case hasUnresolvedIntent:
		return "has unresolved intent"
	case hasObservedDrift:
		return "has drift"
	default:
		return "in sync"
	}
}
