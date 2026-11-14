package resolve

import (
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
		} else if !candidate.PrimaryFile && !keepsLockedFile(request, candidate) {
			// Only the primary file answers an unconstrained requirement. The
			// lock records the file an earlier resolution or adoption chose,
			// and that variant must stay eligible so a later resolution does
			// not silently swap in the release's primary file.
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

// keepsLockedFile reports whether the candidate is the file the lock selected
// for the request's project. Providers without file identities answer false.
func keepsLockedFile(
	request projectRequest,
	candidate upstream.Candidate,
) bool {
	return request.previousFileID != "" &&
		candidate.FileID == request.previousFileID
}

// orderCandidates applies the cache order: newest publication first, then the
// previously locked build. The release-channel preference depends on each
// request's selectors, so the caller applies it after reading the cache.
func orderCandidates(
	list []upstream.Candidate,
	previous []document.Package,
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
	requirements []document.RequirementBinding,
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

// candidateProvenance records the metadata a provider could not establish for
// one candidate. It follows the selected artifact so resolution can report the
// gap instead of letting missing metadata read as a negative answer.
type candidateProvenance struct {
	dependenciesUnknown  bool
	compatibilityUnknown bool
}

// provenanceKey is the persisted candidate identity. URL is deliberately
// excluded: merging selections keeps the artifact identity, not any mirror
// the provider happened to return.
func provenanceKey(
	provider catalog.Provider,
	ecosystem catalog.Ecosystem,
	projectID, releaseID, fileID string,
) string {
	return provider.String() + "|" + ecosystem.String() + "|" +
		projectID + "|" + releaseID + "|" + fileID
}

func packageProvenanceKey(p document.Package) string {
	return provenanceKey(p.Provider, p.Ecosystem, p.ProjectID, p.ReleaseID, p.FileID)
}

// packageReference prefers the declared reference, which keeps a provider's
// canonical project ID from replacing the identity the user wrote.
func packageReference(p document.Package) string {
	for _, requirement := range p.Requirements {
		if requirement.Reference != "" {
			return requirement.Reference
		}
	}
	return p.Provider.String() + ":" + p.ProjectID
}

// provenanceWarnings reports the metadata gaps of the selected builds. A
// provider that cannot answer must be distinguishable from one that answered
// "none".
func provenanceWarnings(
	packages []document.Package,
	provenance map[string]candidateProvenance,
	target Target,
	expandDependencies bool,
) []error {
	var warnings []error
	for _, p := range packages {
		entry, known := provenance[packageProvenanceKey(p)]
		if !known {
			continue
		}
		reference := packageReference(p)
		if entry.dependenciesUnknown && expandDependencies {
			warnings = append(warnings, fmt.Errorf(
				"%s %s: %s does not report dependency metadata for this build; upstream dependencies were not expanded",
				reference,
				p.Version,
				p.Provider.Title(),
			))
		}
		if entry.compatibilityUnknown {
			warnings = append(warnings, fmt.Errorf(
				"%s %s: %s cannot verify compatibility with Minecraft %s (%s); resolution selected it anyway",
				reference,
				p.Version,
				p.Provider.Title(),
				target.Minecraft,
				p.Ecosystem,
			))
		}
	}
	return warnings
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
	chosen map[projectKey]document.Package,
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

// packageConflictWarnings reports the metadata-declared incompatibilities
// among the packages a resolution finally selected. It scans every selected
// package's declarations against every other selection, so a conflict declared
// by either side is reported whichever package the search selected first.
// Findings are deduplicated as unordered pairs and sorted, so repeated
// resolutions report the same warnings in the same order.
func packageConflictWarnings(
	packages []document.Package,
	declared map[projectKey][]upstream.ProjectDependency,
) []error {
	selected := make(map[projectKey]document.Package, len(packages))
	for _, p := range packages {
		selected[projectKey{p.Environment, p.Ecosystem, p.Provider, p.ProjectID}] = p
	}
	seen := make(map[string]struct{})
	var details []string
	for key, conflicts := range declared {
		owner, ok := selected[key]
		if !ok || owner.Artifact.Filename == "" {
			continue
		}
		ownerRef := owner.Provider.String() + ":" + owner.ProjectID
		for _, dep := range conflicts {
			if dep.Provider == owner.Provider && dep.ProjectID == owner.ProjectID {
				continue
			}
			target, ok := selected[projectKey{key.environment, key.ecosystem, dep.Provider, dep.ProjectID}]
			if !ok || target.Artifact.Filename == "" {
				continue
			}
			if dep.ReleaseID != "" && target.ReleaseID != dep.ReleaseID {
				continue
			}
			left, right := ownerRef, conflictDetail(dep)
			if strings.Compare(left, right) > 0 {
				left, right = right, left
			}
			pair := left + "\x00" + right
			if _, ok := seen[pair]; ok {
				continue
			}
			seen[pair] = struct{}{}
			details = append(details, left+" conflicts with "+right)
		}
	}
	slices.Sort(details)
	warnings := make([]error, 0, len(details))
	for _, detail := range details {
		warnings = append(warnings, fmt.Errorf("package conflict: %s", detail))
	}
	return warnings
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
