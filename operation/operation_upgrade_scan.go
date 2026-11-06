package operation

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

func (s *Service) upgradeVersions(
	ctx context.Context,
	root upgradeRoot,
	old *document.Lock,
	maxVersions int,
) ([]upgradeVersion, int, int, bool, error) {
	registry, ok := root.Provider.Registry()
	if !ok {
		return nil, 0, 0, false, fmt.Errorf("provider %s has no package catalogue", root.Provider)
	}
	list, err := s.source.Candidates(
		ctx,
		registry,
		root.Project,
		string(old.Server.Minecraft),
		root.Ecosystem,
	)
	if err != nil {
		return nil, 0, 0, false, err
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
				return nil, 0, 0, false, err
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
	lockedMissing := lockedCandidate == nil
	if lockedMissing {
		// The catalogue no longer serves the locked build. A mutable upstream
		// location can still be upgraded safely: its URL now resolves to a
		// different release, so the lock's bytes are gone rather than the
		// build being quietly re-served under its old identity. Only builds
		// that location currently serves may replace it; unrelated historical
		// builds are never upgrade targets, and any other absence has no safe
		// replacement, so keep failing.
		artifactEligible = upgradeLockedLocationCandidates(root.Previous, artifactEligible)
		if len(artifactEligible) == 0 {
			return nil, 0, 0, false, fmt.Errorf("locked version %s is no longer listed", root.Previous.Version)
		}
		lockedCandidate = upgradeLockedBaseline(root.Previous)
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
		return nil, 0, 0, false, fmt.Errorf("no eligible builds for Minecraft %s", old.Server.Minecraft)
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
		if lockedMissing {
			continue
		}
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
		if !lockedMissing {
			return nil, 0, 0, false, fmt.Errorf("locked version %s is no longer listed", root.Previous.Version)
		}
		// The locked build sits below every listed build: all of them are
		// candidates to move forward to, never a downgrade target.
		lockedRank = len(versions)
	}
	trimmed := versions
	lockedIndex := lockedRank
	if lockedMissing {
		if len(versions) > maxVersions {
			trimmed = versions[:maxVersions]
		}
		lockedIndex = len(trimmed) - 1
		lockedRank = len(trimmed)
		return trimmed, lockedIndex, lockedRank, true, nil
	}
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
	return trimmed, lockedIndex, lockedRank, lockedIndex > 0, nil
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

func upgradeLockedBuild(candidate upstream.Candidate, previous *document.Package) bool {
	if previous == nil {
		return false
	}
	if previous.ReleaseID != "" || previous.FileID != "" {
		return candidate.ReleaseID == previous.ReleaseID && candidate.FileID == previous.FileID
	}
	return candidate.Version == previous.Version
}

// upgradeLockedLocationCandidates keeps only candidates served from the locked
// artifact's URL. That URL is a mutable upstream location when it resolves to a
// different release: the locked build's bytes are no longer retrievable under
// their pinned identity, while the location's current builds are. A build that
// simply vanished from an immutable catalogue leaves no candidates.
func upgradeLockedLocationCandidates(
	previous *document.Package,
	candidates []upstream.Candidate,
) []upstream.Candidate {
	if previous == nil || previous.URL == "" {
		return nil
	}
	kept := make([]upstream.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if candidate.URL == previous.URL {
			kept = append(kept, candidate)
		}
	}
	return kept
}

// upgradeLockedBaseline reconstructs the locked build from the validated lock
// for comparison and ranking only. It deliberately carries no URL, filename,
// hashes, or primary-file claim: the catalogue no longer serves this build, so
// it must never be selected, materialized, or presented as a retrievable
// historical artifact.
func upgradeLockedBaseline(previous *document.Package) *upstream.Candidate {
	return &upstream.Candidate{
		Artifact: catalog.Artifact{
			Provider:  previous.Provider,
			ProjectID: previous.ProjectID,
			ReleaseID: previous.ReleaseID,
			FileID:    previous.FileID,
			Version:   previous.Version,
		},
	}
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
