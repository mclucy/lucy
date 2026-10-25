package resolve

import (
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/mclucy/lucy/catalog"
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
	// Remote release hints are advisory. They order candidates, but a build
	// the dependent did not name can still satisfy the dependency.
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
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].PublishedAt.After(ordered[j].PublishedAt)
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

// preferReleaseHints moves candidates named by remote dependency metadata to
// the front of the candidate list. The hints are advisory: a different build
// can still satisfy the dependency.
func preferReleaseHints(
	list []upstream.Candidate,
	hints []releaseHint,
) []upstream.Candidate {
	hinted := map[string]bool{}
	for _, hint := range hints {
		if hint.releaseID != "" {
			hinted[hint.releaseID] = true
		}
	}
	if len(hinted) == 0 {
		return list
	}
	ordered := make([]upstream.Candidate, 0, len(list))
	for _, candidate := range list {
		if hinted[candidate.ReleaseID] {
			ordered = append(ordered, candidate)
		}
	}
	for _, candidate := range list {
		if !hinted[candidate.ReleaseID] {
			ordered = append(ordered, candidate)
		}
	}
	return ordered
}

// conflictDependencies returns the candidate's incompatibility declarations.
func conflictDependencies(candidate upstream.Candidate) []upstream.ProjectDependency {
	var out []upstream.ProjectDependency
	for _, dep := range candidate.Dependencies {
		switch dep.Kind {
		case catalog.DependencyKindIncompatible, catalog.DependencyKindConflict:
			out = append(out, dep)
		}
	}
	return out
}

// metadataConflict reports the first metadata-declared incompatibility between
// candidate and the selected packages. It checks both directions so a one-sided
// declaration is not selection-order dependent.
func metadataConflict(
	candidate upstream.Candidate,
	request projectRequest,
	chosen map[projectKey]lockfile.Package,
	conflicts map[projectKey][]upstream.ProjectDependency,
) (string, bool) {
	best := ""
	note := func(detail string) {
		if best == "" || strings.Compare(detail, best) < 0 {
			best = detail
		}
	}
	for _, dep := range conflictDependencies(candidate) {
		if dep.Provider == candidate.Provider && dep.ProjectID == candidate.ProjectID {
			continue
		}
		for key, selected := range chosen {
			if key.environment != request.key.environment || key.ecosystem != request.key.ecosystem {
				continue
			}
			if selected.Artifact.Filename == "" ||
				selected.Provider != dep.Provider ||
				selected.ProjectID != dep.ProjectID {
				continue
			}
			if dep.ReleaseID != "" && selected.ReleaseID != dep.ReleaseID {
				continue
			}
			note(conflictDetail(dep))
		}
	}
	for key, selectedConflicts := range conflicts {
		if key.environment != request.key.environment || key.ecosystem != request.key.ecosystem {
			continue
		}
		for _, dep := range selectedConflicts {
			if dep.Provider != candidate.Provider || dep.ProjectID != candidate.ProjectID {
				continue
			}
			if dep.ReleaseID != "" && candidate.ReleaseID != dep.ReleaseID {
				continue
			}
			note(conflictDetail(dep))
		}
	}
	return best, best != ""
}

func conflictDetail(dep upstream.ProjectDependency) string {
	detail := dep.Provider.String() + ":" + dep.ProjectID
	if dep.ReleaseID != "" {
		detail += "@" + dep.ReleaseID
	}
	return detail
}

type analysisEntry struct {
	modules []catalog.DescriptorModule
	hashes  map[string]string
}

// AnalysisCache reuses artifact analysis within one resolution session.
type AnalysisCache struct {
	entries map[string]analysisEntry
}

func NewAnalysisCache() *AnalysisCache {
	return &AnalysisCache{entries: map[string]analysisEntry{}}
}

func (c *AnalysisCache) get(key string) (analysisEntry, bool) {
	if c == nil {
		return analysisEntry{}, false
	}
	entry, ok := c.entries[key]
	if !ok {
		return analysisEntry{}, false
	}
	return analysisEntry{
		modules: cloneModules(entry.modules),
		hashes:  entry.hashes,
	}, true
}

func (c *AnalysisCache) put(key string, entry analysisEntry) {
	if c == nil {
		return
	}
	c.entries[key] = analysisEntry{
		modules: cloneModules(entry.modules),
		hashes:  entry.hashes,
	}
}

func candidateAnalysisKey(candidate upstream.Candidate) string {
	return string(candidate.Provider) + "|" +
		candidate.ProjectID + "|" +
		candidate.ReleaseID + "|" +
		candidate.FileID + "|" +
		candidate.URL
}

func cloneModules(modules []catalog.DescriptorModule) []catalog.DescriptorModule {
	if len(modules) == 0 {
		return nil
	}
	out := make([]catalog.DescriptorModule, len(modules))
	for i, module := range modules {
		out[i] = module
		out[i].Provides = slices.Clone(module.Provides)
		out[i].Dependencies = slices.Clone(module.Dependencies)
	}
	return out
}
