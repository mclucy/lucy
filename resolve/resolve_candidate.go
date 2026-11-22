package resolve

import (
	"fmt"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"

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
	pin *document.Package,
) []upstream.Candidate {
	ordered := slices.Clone(list)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].PublishedAt.After(ordered[j].PublishedAt)
	})
	if pin != nil {
		for i, candidate := range ordered {
			if candidate.ReleaseID == pin.ReleaseID && candidate.FileID == pin.FileID {
				copy(ordered[1:i+1], ordered[:i])
				ordered[0] = candidate
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
	if len(list) < 2 {
		return list
	}
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
// declaration behaves the same in either selection order.
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

// AnalysisCache reuses artifact analysis within one resolution session. The
// cache is safe for concurrent use, because speculative analysis runs beside
// the search that may consume it.
type AnalysisCache struct {
	mu      sync.Mutex
	entries map[analysisKey]analysisEntry
	content map[analysisContentKey]analysisEntry
}

func NewAnalysisCache() *AnalysisCache {
	return &AnalysisCache{
		entries: make(map[analysisKey]analysisEntry),
		content: make(map[analysisContentKey]analysisEntry),
	}
}

// has reports whether the cache holds analysis for key. It copies nothing.
func (c *AnalysisCache) has(key analysisKey, expected map[string]string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, hit := c.lookupLocked(key, expected)
	return hit
}

// RecordVerified records descriptor modules of an artifact whose bytes the
// caller already verified and made available for replay.
func (c *AnalysisCache) RecordVerified(candidate upstream.Candidate, target Target, ecosystem catalog.Ecosystem, modules []catalog.DescriptorModule, hashes map[string]string) {
	key := analysisKeyFor(candidate, target.DescriptorLoader(ecosystem), target.Minecraft)
	c.put(key, analysisEntry{modules: modules, hashes: hashes})
}

// SeedPrevious records the analysis a lock already carries. A locked package
// names its artifact and hashes, so a pinned candidate whose lock recorded its
// modules needs no materialization or reopen. Packages without recorded
// modules are skipped: an absent module list does not prove the artifact has
// none. A descriptor reader selects its files by loader and Minecraft
// version, so a package resolved for previousTarget is seeded only when both
// match target.
func (c *AnalysisCache) SeedPrevious(previous []document.Package, previousTarget, target Target) {
	if previousTarget.Minecraft == "" || previousTarget.Minecraft != target.Minecraft {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range previous {
		p := &previous[i]
		if p.Artifact.Filename == "" || len(p.Artifact.Modules) == 0 {
			continue
		}
		loader := target.DescriptorLoader(p.Ecosystem)
		if previousTarget.DescriptorLoader(p.Ecosystem) != loader {
			continue
		}
		candidate := upstream.Candidate{Artifact: p.Artifact}
		key := analysisKeyFor(candidate, loader, target.Minecraft)
		// A shared cache may seed the same lock again for another search.
		if _, hit := c.lookupLocked(key, p.Artifact.Hashes); hit {
			continue
		}
		c.putLocked(key, analysisEntry{
			modules: p.Artifact.Modules,
			hashes:  p.Artifact.Hashes,
		})
	}
}

func (c *AnalysisCache) get(key analysisKey, expected map[string]string) (analysisEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, hit := c.lookupLocked(key, expected)
	if !hit {
		return analysisEntry{}, false
	}
	entry.modules = cloneModules(entry.modules)
	return entry, true
}

// lookupLocked resolves an entry without copying its modules. The caller
// holds the cache mutex.
func (c *AnalysisCache) lookupLocked(key analysisKey, expected map[string]string) (analysisEntry, bool) {
	entry, hit := c.entries[key]
	if hit && matchesAnalysisHashes(entry.hashes, expected) {
		return analysisEntry{modules: entry.modules, hashes: entry.hashes}, true
	}
	if expected["sha256"] == "" {
		return analysisEntry{}, false
	}
	entry, hit = c.content[key.contentKey(expected["sha256"])]
	if !hit || !matchesAnalysisHashes(entry.hashes, expected) {
		return analysisEntry{}, false
	}
	hashes := make(map[string]string, len(expected))
	for algorithm := range expected {
		hashes[algorithm] = entry.hashes[algorithm]
	}
	return analysisEntry{modules: entry.modules, hashes: hashes}, true
}

func matchesAnalysisHashes(actual, expected map[string]string) bool {
	for algorithm, digest := range expected {
		if value, found := actual[algorithm]; !found || digest != "" && !strings.EqualFold(value, digest) {
			return false
		}
	}
	return true
}

func (c *AnalysisCache) put(key analysisKey, entry analysisEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.putLocked(key, entry)
}

// putLocked stores an entry. The caller holds the cache mutex.
func (c *AnalysisCache) putLocked(key analysisKey, entry analysisEntry) {
	entry.modules = cloneModules(entry.modules)
	c.entries[key] = entry
	if digest := entry.hashes["sha256"]; digest != "" {
		c.content[key.contentKey(digest)] = entry
	}
}

type analysisKey struct {
	provider                              catalog.Provider
	project, release, file, url, filename string
	loader                                catalog.Ecosystem
	minecraft                             versionpkg.Version
}

type analysisContentKey struct {
	digest, extension string
	loader            catalog.Ecosystem
	minecraft         versionpkg.Version
}

func (key analysisKey) contentKey(digest string) analysisContentKey {
	return analysisContentKey{
		digest:    strings.ToLower(digest),
		extension: strings.ToLower(filepath.Ext(key.filename)),
		loader:    key.loader,
		minecraft: key.minecraft,
	}
}

func analysisKeyFor(c upstream.Candidate, loader catalog.Ecosystem, minecraft versionpkg.Version) analysisKey {
	return analysisKey{c.Provider, c.ProjectID, c.ReleaseID, c.FileID, c.URL, c.Filename, loader, minecraft}
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
