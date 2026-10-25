package resolve

import (
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/upstream"

	"github.com/mclucy/lucy/lockfile"

	versionpkg "github.com/mclucy/lucy/version"
)

// candidateDecision records the first reason a candidate cannot answer a
// request. A later rule may also fail; the first failure is the one reported.
type candidateDecision struct {
	eligible bool
	kind     RejectionKind
	detail   string
	reason   string
}

// decideCandidate applies the request's hard filters to one candidate.
func decideCandidate(
	candidate upstream.Candidate,
	request projectRequest,
) (candidateDecision, error) {
	decision := candidateDecision{eligible: true}
	reject := func(kind RejectionKind, detail string) {
		decision.eligible = false
		if decision.reason == "" {
			decision.reason = describeRejection(kind, detail)
			decision.kind = kind
			decision.detail = detail
		}
	}
	for _, hint := range request.releaseHints {
		if hint.releaseID != "" && candidate.ReleaseID != hint.releaseID {
			reject(RejectionReleaseMismatch, hint.releaseID)
		}
	}
	constrained := false
	for _, requirement := range request.requirements {
		if !requirement.Enabled {
			continue
		}
		constrained = true
		switch requirement.Selector {
		case versionpkg.Stable:
			if candidate.Channel != upstream.ReleaseChannelRelease {
				reject(RejectionChannel, "requested a release build")
			}
		case versionpkg.Any, versionpkg.Beta, "":
		default:
			if candidate.Version != requirement.Selector &&
				candidate.ReleaseID != string(requirement.Selector) &&
				candidate.FileID != string(requirement.Selector) &&
				candidate.Filename != string(requirement.Selector) {
				reject(RejectionVersionMismatch, string(requirement.Selector))
			}
		}
		if requirement.FilePattern != "" {
			match, err := filepath.Match(requirement.FilePattern, candidate.Filename)
			if err != nil {
				return decision, err
			}
			if !match {
				reject(RejectionFilePattern, requirement.FilePattern)
			}
		} else if !candidate.PrimaryFile {
			reject(RejectionPrimaryFile, "")
		}
	}
	// A dependency request carries no requirement of its own, so no enabled
	// binding rejects the extra files a release publishes: the sources and dev
	// jars carry a readable descriptor and would otherwise win the version race
	// on publication date alone.
	if !constrained && !candidate.PrimaryFile {
		reject(RejectionPrimaryFile, "")
	}
	return decision, nil
}

// orderCandidates applies the cache order: newest publication first, then the
// previously locked build. The release-channel preference depends on each
// request's selectors, so the caller applies it after reading the cache.
func orderCandidates(
	list []upstream.Candidate,
	previous []lockfile.Package,
	request projectRequest,
) []upstream.Candidate {
	ordered := slices.Clone(list)
	slices.SortStableFunc(ordered, func(a, b upstream.Candidate) int {
		return b.PublishedAt.Compare(a.PublishedAt)
	})
	for _, p := range previous {
		if p.Environment != request.key.environment ||
			p.Ecosystem != request.key.ecosystem ||
			p.Provider != request.key.provider ||
			p.ProjectID != request.key.project {
			continue
		}
		for i, candidate := range ordered {
			if candidate.ReleaseID == p.ReleaseID && candidate.FileID == p.FileID {
				ordered = append(
					[]upstream.Candidate{candidate},
					append(ordered[:i], ordered[i+1:]...)...,
				)
				break
			}
		}
	}
	return ordered
}

// preferReleaseBuilds applies the default selector's preference: `any` takes
// the newest build but prefers a release over a prerelease when both exist.
// `beta` keeps publication order because it accepts prereleases outright, and
// `stable` rejects prereleases in the candidate loop.
func preferReleaseBuilds(
	list []upstream.Candidate,
	requirements []lockfile.RequirementBinding,
) []upstream.Candidate {
	prefer := false
	for _, requirement := range requirements {
		if !requirement.Enabled {
			continue
		}
		if requirement.Selector == versionpkg.Any || requirement.Selector == "" {
			prefer = true
		}
	}
	if !prefer {
		return list
	}
	ordered := make([]upstream.Candidate, 0, len(list))
	for _, candidate := range list {
		if candidate.Channel == upstream.ReleaseChannelRelease {
			ordered = append(ordered, candidate)
		}
	}
	for _, candidate := range list {
		if candidate.Channel != upstream.ReleaseChannelRelease {
			ordered = append(ordered, candidate)
		}
	}
	return ordered
}
