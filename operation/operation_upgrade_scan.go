package operation

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
	versionpkg "github.com/mclucy/lucy/version"
)

func upgradeVersions(
	ctx context.Context,
	root upgradeRoot,
	old *lockfile.Document,
	maxVersions int,
) ([]upgradeVersion, int, int, error) {
	registry, ok := root.Provider.Registry()
	if !ok {
		return nil, 0, 0, fmt.Errorf("provider %s has no package catalogue", root.Provider)
	}
	list, err := routing.Candidates(
		ctx,
		registry,
		root.Project,
		string(old.Server.Minecraft),
		root.Ecosystem,
	)
	if err != nil {
		return nil, 0, 0, err
	}
	selector := root.Requirement.Selector
	artifactEligible := make([]upstream.Candidate, 0, len(list))
	for _, candidate := range list {
		if upgradeLockedBuild(candidate, root.Previous) {
			artifactEligible = append(artifactEligible, candidate)
			continue
		}
		if root.Requirement.FilePattern != "" {
			match, err := filepath.Match(root.Requirement.FilePattern, candidate.Filename)
			if err != nil {
				return nil, 0, 0, err
			}
			if !match {
				continue
			}
		} else if !candidate.PrimaryFile {
			continue
		}
		artifactEligible = append(artifactEligible, candidate)
	}
	var lockedCandidate *upstream.Candidate
	for i := range artifactEligible {
		if upgradeLockedBuild(artifactEligible[i], root.Previous) {
			lockedCandidate = &artifactEligible[i]
			break
		}
	}
	if lockedCandidate == nil {
		return nil, 0, 0, fmt.Errorf("locked version %s is no longer listed", root.Previous.Version)
	}
	dialect := upgradeArtifactDialect(root.Ecosystem)
	withoutDowngrades := make([]upstream.Candidate, 0, len(artifactEligible))
	for _, candidate := range artifactEligible {
		if upgradeLockedBuild(candidate, root.Previous) {
			withoutDowngrades = append(withoutDowngrades, candidate)
			continue
		}
		if atLeast, comparable := upgradeVersionAtLeast(candidate.Version, lockedCandidate.Version, dialect); comparable {
			if !atLeast {
				continue
			}
		} else if !candidate.PublishedAt.IsZero() && !lockedCandidate.PublishedAt.IsZero() &&
			candidate.PublishedAt.Before(lockedCandidate.PublishedAt) {
			continue
		}
		withoutDowngrades = append(withoutDowngrades, candidate)
	}
	hasRelease := false
	for _, candidate := range withoutDowngrades {
		if candidate.Channel == upstream.ReleaseChannelRelease {
			hasRelease = true
			break
		}
	}
	eligible := make([]upstream.Candidate, 0, len(withoutDowngrades))
	for _, candidate := range withoutDowngrades {
		locked := upgradeLockedBuild(candidate, root.Previous)
		if !locked && selector == versionpkg.Stable && candidate.Channel != upstream.ReleaseChannelRelease {
			continue
		}
		if !locked &&
			(selector == versionpkg.Any || selector == "" || !selector.CanInfer()) &&
			hasRelease && candidate.Channel != upstream.ReleaseChannelRelease {
			continue
		}
		eligible = append(eligible, candidate)
	}
	if len(eligible) == 0 {
		return nil, 0, 0, fmt.Errorf("no eligible builds for Minecraft %s", old.Server.Minecraft)
	}
	byVersion := map[versionpkg.Version][]upstream.Candidate{}
	for _, candidate := range eligible {
		byVersion[candidate.Version] = append(byVersion[candidate.Version], candidate)
	}
	versions := make([]upgradeVersion, 0, len(byVersion))
	for version, builds := range byVersion {
		sort.SliceStable(builds, func(i, j int) bool {
			a, b := &builds[i], &builds[j]
			if n := b.PublishedAt.Compare(a.PublishedAt); n != 0 {
				return n < 0
			}
			if n := strings.Compare(a.ReleaseID, b.ReleaseID); n != 0 {
				return n < 0
			}
			return a.FileID < b.FileID
		})
		versions = append(versions, upgradeVersion{
			Version:     version,
			Builds:      builds,
			PublishedAt: builds[0].PublishedAt,
		})
	}
	sort.Slice(versions, func(i, j int) bool {
		a, b := &versions[i], &versions[j]
		if n := b.PublishedAt.Compare(a.PublishedAt); n != 0 {
			return n < 0
		}
		return string(b.Version) < string(a.Version)
	})
	lockedRank := -1
	for i := range versions {
		versions[i].Rank = i
		if versions[i].Version == root.Previous.Version {
			lockedRank = i
			continue
		}
		for _, build := range versions[i].Builds {
			if build.ReleaseID == root.Previous.ReleaseID && build.FileID == root.Previous.FileID {
				lockedRank = i
			}
		}
	}
	if lockedRank < 0 {
		return nil, 0, 0, fmt.Errorf("locked version %s is no longer listed", root.Previous.Version)
	}
	trimmed := versions
	lockedIndex := lockedRank
	if len(versions) > maxVersions {
		if lockedRank < maxVersions {
			trimmed = versions[:maxVersions]
			lockedIndex = lockedRank
		} else {
			trimmed = append([]upgradeVersion{}, versions[:maxVersions-1]...)
			trimmed = append(trimmed, versions[lockedRank])
			lockedIndex = len(trimmed) - 1
		}
	}
	return trimmed, lockedIndex, lockedRank, nil
}

func upgradeCombinations(roots []upgradeRoot, active []int, limit int) [][]int {
	locked := make([]int, len(active))
	for i, rootIndex := range active {
		locked[i] = roots[rootIndex].LockedIndex
	}
	seeds := []int{}
	for range active {
		seeds = append(seeds, 0)
	}
	queue := [][]int{seeds, locked}
	seen := map[string]bool{}
	var combos [][]int
	for len(queue) > 0 && len(combos) < limit {
		combo := queue[0]
		queue = queue[1:]
		key := fmt.Sprint(combo)
		if seen[key] {
			continue
		}
		seen[key] = true
		combos = append(combos, combo)
		for i, rootIndex := range active {
			if combo[i]+1 > roots[rootIndex].LockedIndex {
				continue
			}
			next := slices.Clone(combo)
			next[i]++
			queue = append(queue, next)
		}
	}
	slices.SortFunc(combos, func(a, b []int) int {
		sumA, sumB := 0, 0
		for i := range a {
			sumA += a[i]
			sumB += b[i]
		}
		if sumA != sumB {
			return sumA - sumB
		}
		for i := range a {
			if a[i] != b[i] {
				return a[i] - b[i]
			}
		}
		return 0
	})
	return combos
}

func upgradeLockedBuild(candidate upstream.Candidate, previous *lockfile.Package) bool {
	if previous == nil {
		return false
	}
	if previous.ReleaseID != "" || previous.FileID != "" {
		return candidate.ReleaseID == previous.ReleaseID && candidate.FileID == previous.FileID
	}
	return candidate.Version == previous.Version
}

func upgradeArtifactDialect(ecosystem catalog.Ecosystem) versionpkg.Dialect {
	switch ecosystem {
	case catalog.EcosystemBukkit, catalog.EcosystemPaper:
		return versionpkg.DialectOpaque
	default:
		return versionpkg.DialectMaven
	}
}

func upgradeVersionAtLeast(
	candidate, locked versionpkg.Version,
	dialect versionpkg.Dialect,
) (bool, bool) {
	if !dialect.Valid() || dialect == versionpkg.DialectOpaque {
		return false, false
	}
	parsedCandidate, err := versionpkg.ParseVersion(candidate, dialect)
	if err != nil {
		return false, false
	}
	parsedLocked, err := versionpkg.ParseVersion(locked, dialect)
	if err != nil {
		return false, false
	}
	comparison, comparable := versionpkg.Compare(parsedCandidate, parsedLocked)
	if !comparable {
		return false, false
	}
	return comparison >= 0, true
}
