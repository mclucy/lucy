package resolve

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/upstream"

	"github.com/mclucy/lucy/artifact"
	serverpkg "github.com/mclucy/lucy/server"

	versionpkg "github.com/mclucy/lucy/version"
)

// Limit failed speculative selections, not successful visits and merges.
const backtrackLimit = 10000

// catalogueRegistry names the registry that serves a package provider. Only a
// provider backing a registry can answer catalogue queries.
func catalogueRegistry(provider catalog.Provider) catalog.Registry {
	registry, ok := provider.Registry()
	if !ok {
		return catalog.RegistryUnspecified
	}
	return registry
}

// selectionState owns the branch-local graph of the package search: the
// chosen packages, the incompatibilities their metadata declares, and the
// packages an exact release demand pins. An artifact and its declarations
// form one selection, so the maps change together. Canonical identities,
// candidate lists, and artifact analysis are session-scoped caches and
// deliberately live outside this state.
type selectionState struct {
	chosen    map[projectKey]document.Package
	conflicts map[projectKey][]upstream.ProjectDependency
	pinned    map[projectKey]bool
}

// searchOutcome reports how exploring one branch of the search ended. A
// failure only this branch reaches leaves the enclosing requests free to try
// another candidate; a failure that holds in every branch stops the search.
type searchOutcome uint8

const (
	// branchFailed means the explored branch cannot settle. The enclosing
	// request may still try its next candidate.
	branchFailed searchOutcome = iota
	// graphSettled means the explored branch produced a complete selection.
	graphSettled
	// searchStopped means the search ended before it settled. Backtracking
	// would only repeat work no candidate choice changes.
	searchStopped
)

// selectionBranch records the entry one change replaced, so restore can put
// it back.
type selectionBranch struct {
	key          projectKey
	previous     document.Package
	hadPrevious  bool
	conflicts    []upstream.ProjectDependency
	hadConflicts bool
	pinned       bool
}

func newSelectionState() selectionState {
	return selectionState{
		chosen:    map[projectKey]document.Package{},
		conflicts: map[projectKey][]upstream.ProjectDependency{},
		pinned:    map[projectKey]bool{},
	}
}

func (s *selectionState) reset() {
	clear(s.chosen)
	clear(s.conflicts)
	clear(s.pinned)
}

func (s *selectionState) selection(key projectKey) (document.Package, bool) {
	p, ok := s.chosen[key]
	return p, ok
}

// extend merges a demand into a selected package. The selection keeps its
// conflicts and pins; an incoming exact-release demand adds a pin.
func (s *selectionState) extend(
	key projectKey,
	p document.Package,
	pin bool,
) selectionBranch {
	return s.install(key, p, s.conflicts[key], s.pinned[key] || pin)
}

func (s *selectionState) install(
	key projectKey,
	p document.Package,
	declared []upstream.ProjectDependency,
	pinned bool,
) selectionBranch {
	branch := s.snapshot(key)
	s.chosen[key] = p
	if len(declared) > 0 {
		s.conflicts[key] = declared
	} else {
		delete(s.conflicts, key)
	}
	if pinned {
		s.pinned[key] = true
	} else {
		delete(s.pinned, key)
	}
	return branch
}

func (s *selectionState) remove(key projectKey) selectionBranch {
	branch := s.snapshot(key)
	delete(s.chosen, key)
	delete(s.conflicts, key)
	delete(s.pinned, key)
	return branch
}

func (s *selectionState) snapshot(key projectKey) selectionBranch {
	previous, hadPrevious := s.chosen[key]
	conflicts, hadConflicts := s.conflicts[key]
	return selectionBranch{
		key:          key,
		previous:     previous,
		hadPrevious:  hadPrevious,
		conflicts:    conflicts,
		hadConflicts: hadConflicts,
		pinned:       s.pinned[key],
	}
}

func (s *selectionState) restore(branch selectionBranch) {
	if branch.hadPrevious {
		s.chosen[branch.key] = branch.previous
	} else {
		delete(s.chosen, branch.key)
	}
	if branch.hadConflicts {
		s.conflicts[branch.key] = branch.conflicts
	} else {
		delete(s.conflicts, branch.key)
	}
	if branch.pinned {
		s.pinned[branch.key] = true
	} else {
		delete(s.pinned, branch.key)
	}
}

// restoreAll undoes branches in reverse order of their changes.
func (s *selectionState) restoreAll(branches []selectionBranch) {
	for i := len(branches) - 1; i >= 0; i-- {
		s.restore(branches[i])
	}
}

// Index both identities: manifest requests use references, dependencies use project IDs.
type lockIndex struct {
	byProject   map[projectKey]*document.Package
	byReference map[projectKey]*document.Package
}

func indexLock(previous []document.Package) lockIndex {
	index := lockIndex{
		byProject:   make(map[projectKey]*document.Package, len(previous)),
		byReference: make(map[projectKey]*document.Package, len(previous)),
	}
	for i := range previous {
		p := &previous[i]
		key := projectKey{p.Environment, p.Ecosystem, p.Provider, p.ProjectID}
		index.byProject[key] = p
		for _, binding := range p.Requirements {
			key.project = binding.Reference
			index.byReference[key] = p
		}
	}
	return index
}

// lockedCandidate rebuilds the candidate the lock accepted for this request.
// An adopted release can carry a compatibility exception that ordinary
// provider filtering drops, so the locked file stays selectable while its
// runtime target is unchanged. The candidate keeps the dependency metadata the
// lock recorded, because the catalogue no longer supplies it.
func lockedCandidate(p *document.Package) (upstream.Candidate, bool) {
	if p == nil || p.Filename == "" {
		return upstream.Candidate{}, false
	}
	return upstream.Candidate{
		Artifact:            p.Artifact,
		PrimaryFile:         true,
		Channel:             p.Channel,
		Dependencies:        candidateDependencies(p.Dependencies),
		DependenciesUnknown: p.DependenciesUnknown,
	}, true
}

// releaseDependencies records a candidate's declared project relationships in
// the lock.
func releaseDependencies(
	deps []upstream.ProjectDependency,
) []document.PackageDependency {
	if len(deps) == 0 {
		return nil
	}
	out := make([]document.PackageDependency, len(deps))
	for i, dep := range deps {
		out[i] = document.PackageDependency{
			Provider:  dep.Provider,
			ProjectID: dep.ProjectID,
			ReleaseID: dep.ReleaseID,
			Kind:      dep.Kind,
		}
	}
	return out
}

// candidateDependencies restores the relationships a lock recorded.
func candidateDependencies(
	deps []document.PackageDependency,
) []upstream.ProjectDependency {
	if len(deps) == 0 {
		return nil
	}
	out := make([]upstream.ProjectDependency, len(deps))
	for i, dep := range deps {
		out[i] = upstream.ProjectDependency{
			Provider:  dep.Provider,
			ProjectID: dep.ProjectID,
			ReleaseID: dep.ReleaseID,
			Kind:      dep.Kind,
		}
	}
	return out
}

// appendLockedCandidate adds the locked file when the catalogue did not
// publish a matching candidate.
func appendLockedCandidate(
	list []upstream.Candidate,
	pin *document.Package,
) []upstream.Candidate {
	locked, ok := lockedCandidate(pin)
	if !ok {
		return list
	}
	locked.CompatibilityUnknown = true
	if slices.ContainsFunc(list, func(candidate upstream.Candidate) bool {
		if locked.FileID != "" && candidate.FileID != "" {
			return candidate.FileID == locked.FileID
		}
		return candidate.Version == locked.Version &&
			candidate.Filename == locked.Filename
	}) {
		return list
	}
	return append(list, locked)
}

func Packages(request PackagesRequest) ([]document.Package, []error, error) {
	ctx := request.Context
	logger := request.Logger
	candidatesFor := request.Candidates
	if ctx == nil {
		ctx = context.Background()
	}
	d := request.Manifest
	previous := indexLock(request.Previous)
	preserveLocked := request.PreserveLocked
	target := request.Target
	materialize := request.Materialize
	withOptional := request.WithOptional
	acceptBroken := request.AcceptBroken
	acceptPackageConflicts := request.AcceptPackageConflicts
	expandDependencies := request.ExpandDependencies
	trace := request.Trace
	var requests []projectRequest
	appendRequest := func(environment serverpkg.Environment, ecosystem catalog.Ecosystem, ref string, r document.Requirement) error {
		provider, project, err := document.ParseReference(ref)
		if err != nil {
			return err
		}
		// The slug the user wrote is the display name even when the lock lets
		// resolution start from the project identity.
		display := project
		fileID := ""
		if pin := previous.byReference[projectKey{environment, ecosystem, provider, ref}]; pin != nil {
			project, fileID = pin.ProjectID, pin.FileID
		}
		requests = append(requests, projectRequest{
			key: projectKey{
				environment,
				ecosystem,
				provider,
				project,
			},
			display:        display,
			origin:         originManifest,
			previousFileID: fileID,
			requirements: []document.RequirementBinding{
				{
					Reference:   ref,
					Selector:    r.Selector,
					FilePattern: r.FilePattern,
					Enabled:     r.IsEnabled(),
				},
			},
		})
		return nil
	}
	var ecosystems []catalog.Ecosystem
	for ecosystem := range d.Server.Packages {
		ecosystems = append(ecosystems, ecosystem)
	}
	slices.Sort(ecosystems)
	for _, ecosystem := range ecosystems {
		var refs []string
		for ref := range d.Server.Packages[ecosystem] {
			refs = append(refs, ref)
		}
		slices.Sort(refs)
		for _, ref := range refs {
			if err := appendRequest(serverpkg.EnvironmentServer, ecosystem, ref, d.Server.Packages[ecosystem][ref]); err != nil {
				return nil, nil, err
			}
		}
	}
	if d.MCDR != nil {
		var refs []string
		for ref := range d.MCDR.Packages {
			refs = append(refs, ref)
		}
		slices.Sort(refs)
		for _, ref := range refs {
			if err := appendRequest(serverpkg.EnvironmentMCDR, catalog.EcosystemMCDR, ref, d.MCDR.Packages[ref]); err != nil {
				return nil, nil, err
			}
		}
	}
	for _, r := range requests {
		if r.key.environment == serverpkg.EnvironmentServer {
			if !SupportsPackages(target, r.key.ecosystem) {
				for _, req := range r.requirements {
					if req.Enabled {
						return nil, nil, fmt.Errorf("product %s cannot load %s packages", target.Product, r.key.ecosystem)
					}
				}
			}
		}
	}
	candidates := map[projectKey][]upstream.Candidate{}
	provenance := map[string]candidateProvenance{}
	canonical := map[projectKey]projectKey{}
	state := newSelectionState()
	cache := request.Cache
	if cache == nil {
		cache = NewAnalysisCache()
	}
	attempts := 0
	backtracks := 0
	preferPins := preserveLocked && request.PreferLocked && !withOptional
	var pinnedEdges map[projectKey][]projectRequest
	if preferPins && expandDependencies {
		pinnedEdges = previous.lockedEdges(requests, request.Previous)
	}
	usedPin := false
	native := make(map[moduleKey]nativeOwner)
	// Non-nil only when a direct requirement exhausted its candidates and
	// the named prior decisions alone suffice to explain every rejection.
	var blockers map[projectKey]bool
	var settled []document.Package
	var problems []ModuleProblem
	strict := true
	var lastErr error
	ledger := newRejectionLedger()
	logger.Info(
		"resolve: resolving package requirements",
		"requests", len(requests),
	)
	// operational marks a failure that no candidate rejection explains, such as
	// a network or checksum error. It outranks the rejection summary.
	operational := false
	// hardFail records a failure scoped to the branch being explored. Another
	// candidate of an enclosing request may still settle a graph.
	hardFail := func(err error) searchOutcome {
		lastErr = err
		operational = true
		return branchFailed
	}
	// stop ends the search because no remaining candidate reaches a different
	// answer. Only a failure that holds in every branch qualifies: a cancelled
	// context, an exhausted search budget, or a manifest requirement that no
	// catalogue answer can satisfy. A dependency requirement exists only
	// because one parent candidate selected it, and another candidate of that
	// parent may not need the project at all, so its failures stay local.
	stop := func(err error) searchOutcome {
		lastErr = err
		operational = true
		return searchStopped
	}
	var backtrack func([]projectRequest) searchOutcome
	backtrack = func(pending []projectRequest) searchOutcome {
		if err := ctx.Err(); err != nil {
			return stop(err)
		}
		attempts++
		trace.state(len(state.chosen), len(pending))
		if len(pending) == 0 {
			all := make([]document.Package, 0, len(state.chosen))
			for _, p := range state.chosen {
				if p.Filename != "" {
					all = append(all, p)
				}
			}
			all = mergeArtifactSelections(all)
			sortPackages(all)
			var err error
			problems, err = finalizeModules(all, target)
			if err != nil {
				logger.Debug(
					"resolve: settled graph invalid",
					"attempt", attempts,
					"reason", err,
				)
				return hardFail(err)
			}
			if len(problems) > 0 && strict {
				fatal := problems
				if acceptPackageConflicts && !acceptBroken {
					fatal = serverModuleProblems(problems)
				}
				if len(fatal) == 0 {
					settled = all
					return graphSettled
				}
				logger.Debug(
					"resolve: settled graph rejected",
					"attempt", attempts,
					"reason", fatal[0],
				)
				lastErr = errors.Join(problemErrors(fatal)...)
				for _, problem := range fatal {
					key := projectKey{
						problem.Environment,
						problem.Ecosystem,
						problem.Provider,
						problem.ProjectID,
					}
					display := ledger.displayFor(key)
					if display == "" {
						display = problem.ProjectID
					}
					version := ""
					if owner, ok := state.selection(key); ok {
						version = string(owner.Version)
					}
					ledger.recordModule(
						key,
						display,
						version,
						problem,
					)
					ledger.markFailed(key)
				}
				return branchFailed
			}
			settled = all
			return graphSettled
		}
		r := pending[0]
		rest := pending[1:]
		// giveUp records the request-level failure that explains why this
		// branch cannot settle.
		giveUp := func() {
			if lastErr == nil {
				lastErr = fmt.Errorf("no candidate satisfies %s:%s", r.key.provider, r.key.project)
			}
			ledger.markFailed(r.key)
		}
		if canonicalKey, ok := canonical[r.key]; ok {
			r.key = canonicalKey
		}
		ledger.noteDisplay(r.key, r.display)
		dependencyDemand := r.dependencyOf.Name != ""
		active := len(r.requirements) == 0
		for _, req := range r.requirements {
			active = active || req.Enabled
		}
		// An artifact-less entry records only a disabled direct binding. When
		// something else in the graph needs the package, that demand outranks
		// the binding, so the artifact resolves instead of merging into a
		// package that no file can satisfy.
		if previous, ok := state.selection(r.key); ok &&
			(!active || previous.Artifact.Filename != "") {
			previous.Requirements = append(previous.Requirements, r.requirements...)
			previous.Dependency = previous.Dependency || dependencyDemand
			if dependencyDemand && !slices.Contains(previous.DependencyOf, r.dependencyOf) {
				previous.DependencyOf = append(previous.DependencyOf, r.dependencyOf)
			}
			branch := state.extend(
				r.key,
				previous,
				len(r.releaseHints) > 0,
			)
			if outcome := backtrack(rest); outcome != branchFailed {
				return outcome
			}
			state.restore(branch)
			logger.Debug(
				"resolve: backtracking requirement merge",
				"provider", r.key.provider,
				"project", r.key.project,
				"reason", "remaining requests failed after merge",
			)
			return branchFailed
		}
		if !active {
			p := document.Package{
				Environment:  r.key.environment,
				Ecosystem:    r.key.ecosystem,
				Provider:     r.key.provider,
				ProjectID:    r.key.project,
				Requirements: r.requirements,
			}
			branch := state.install(r.key, p, nil, false)
			if outcome := backtrack(rest); outcome != branchFailed {
				return outcome
			}
			state.restore(branch)
			return branchFailed
		}
		// A disabled direct binding may already hold an artifact-less entry
		// here, because a real demand outranks it. Selecting a file replaces
		// the entry, so the binding's requirements have to travel into the
		// selection and back out again when the selection does not resolve.
		var artifactless document.Package
		replacingArtifactless := false
		if prior, ok := state.selection(r.key); ok && prior.Artifact.Filename == "" {
			artifactless, replacingArtifactless = prior, true
		}
		list, ok := candidates[r.key]
		prior := previous.byProject[r.key]
		replayEdges := false
		if preferPins {
			if pin, found := lockedCandidate(prior); found {
				decision, err := decideCandidate(pin, r)
				if err != nil {
					return hardFail(err)
				}
				if decision.eligible && (len(r.releaseHints) == 0 || r.releaseHints[0].releaseID == pin.ReleaseID) {
					list, ok = []upstream.Candidate{pin}, true
					usedPin = true
					replayEdges = pinnedEdges != nil
					ledger.setCandidates(r.key, r.display, 1)
				}
			}
		}
		if !ok {
			if candidatesFor == nil {
				return stop(errors.New("resolution requires a candidate source"))
			}
			var err error
			list, err = candidatesFor(ctx, catalogueRegistry(r.key.provider), r.key.project, string(target.Minecraft), r.key.ecosystem)
			if err != nil {
				var locked upstream.Candidate
				lockedOK := false
				if preserveLocked {
					locked, lockedOK = lockedCandidate(prior)
					locked.CompatibilityUnknown = true
				}
				if !lockedOK {
					// A manifest requirement holds in every branch, so a
					// catalogue that cannot list it ends the search. A
					// dependency requirement can still be avoided by another
					// candidate of its parent.
					if r.origin == originManifest {
						return stop(err)
					}
					return hardFail(err)
				}
				logger.Debug(
					"resolve: catalogue failed; keeping locked file",
					"provider", r.key.provider,
					"project", r.key.project,
					"reason", err,
				)
				list = []upstream.Candidate{locked}
			} else if preserveLocked {
				list = appendLockedCandidate(list, prior)
			}
			list = orderCandidates(list, prior)
			candidates[r.key] = list
			ledger.setCandidates(r.key, r.display, len(list))
		}
		if len(list) > 0 {
			// Direct requirements name a project by its catalogue slug, while
			// catalogue metadata names the same project by identity. Resolve
			// under the identity so the project enters the graph only once.
			if project := list[0].ProjectID; project != "" && project != r.key.project {
				canonicalKey := r.key
				canonicalKey.project = project
				canonical[r.key] = canonicalKey
				if _, cached := candidates[canonicalKey]; !cached {
					candidates[canonicalKey] = list
				}
				ledger.setCandidates(canonicalKey, r.display, len(list))
				logger.Debug(
					"resolve: project identity canonicalized",
					"provider", r.key.provider,
					"reference", r.key.project,
					"project", project,
				)
				pending[0].key = canonicalKey
				return backtrack(pending)
			}
		}
		if len(list) == 0 {
			ledger.record(r.key, r.display, "", "no published builds match this server", RejectionEmpty)
			giveUp()
			if r.origin == originManifest {
				// Nothing this project published satisfies a requirement that
				// holds in every branch.
				return searchStopped
			}
			return branchFailed
		}
		list = preferReleaseBuilds(list, r.requirements)
		list = preferReleaseHints(list, r.releaseHints)
		eligible := false
		explained := r.origin == originManifest
		var causes map[projectKey]bool
		triedBranch := false
		descend := func(next []projectRequest) searchOutcome {
			if triedBranch {
				backtracks++
				if backtracks > backtrackLimit {
					return stop(&SearchLimitError{Attempts: backtrackLimit})
				}
			}
			triedBranch = true
			return backtrack(next)
		}
		for _, candidate := range list {
			if err := ctx.Err(); err != nil {
				return stop(err)
			}
			decision, err := decideCandidate(candidate, r)
			if err != nil {
				return hardFail(err)
			}
			if !decision.eligible {
				ledger.record(r.key, r.display, string(candidate.Version), decision.detail, decision.kind)
				trace.rejected(r.display, string(candidate.Version), decision.reason)
				logger.Debug(
					"resolve: candidate rejected",
					"provider", r.key.provider,
					"project", r.key.project,
					"origin", r.origin.String(),
					"candidate", candidate.Version,
					"file", candidate.Filename,
					"reason", decision.reason,
				)
				continue
			}
			eligible = true
			if detail, conflicting := metadataConflict(candidate, r, state.chosen, state.conflicts); conflicting {
				if !acceptPackageConflicts {
					explained = false
					reason := describeRejection(RejectionMetadataConflict, detail)
					ledger.record(r.key, r.display, string(candidate.Version), detail, RejectionMetadataConflict)
					trace.rejected(r.display, string(candidate.Version), reason)
					logger.Debug(
						"resolve: candidate rejected",
						"provider", r.key.provider,
						"project", r.key.project,
						"origin", r.origin.String(),
						"candidate", candidate.Version,
						"file", candidate.Filename,
						"reason", reason,
					)
					continue
				}
				logger.Debug(
					"resolve: metadata conflict accepted",
					"provider", r.key.provider,
					"project", r.key.project,
					"origin", r.origin.String(),
					"candidate", candidate.Version,
					"file", candidate.Filename,
					"conflict", detail,
				)
			}
			a := candidate.Artifact
			if candidate.DependenciesUnknown || candidate.CompatibilityUnknown {
				provenance[provenanceKey(
					candidate.Provider,
					r.key.ecosystem,
					candidate.ProjectID,
					candidate.ReleaseID,
					candidate.FileID,
				)] = candidateProvenance{
					dependenciesUnknown:  candidate.DependenciesUnknown,
					compatibilityUnknown: candidate.CompatibilityUnknown,
				}
			}
			var modules []catalog.DescriptorModule
			descriptorLoader := target.descriptorLoader(r.key.ecosystem)
			key := analysisKeyFor(candidate, descriptorLoader, target.Minecraft)
			entry, cached := cache.get(key, a.Hashes)
			if cached {
				a.Hashes = entry.hashes
				modules = entry.modules
			} else {
				materialized, err := materialize(&a)
				if err != nil {
					return hardFail(err)
				}
				a.Hashes = materialized.Hashes
				modules, err = artifact.ReadModulesFor(
					ctx,
					materialized.Path,
					descriptorLoader,
					string(target.Minecraft),
				)
				if err != nil {
					explained = false
					// A descriptor the reader cannot interpret rejects this
					// candidate. It is content evidence, not an operational
					// failure of the whole search.
					message := fmt.Sprintf(
						"artifact %s has an unreadable %s descriptor: %v",
						a.Filename,
						descriptorLoader,
						err,
					)
					lastErr = err
					ledger.record(r.key, r.display, string(candidate.Version), message, RejectionModules)
					trace.rejected(r.display, string(candidate.Version), message)
					continue
				}
				cache.put(key, analysisEntry{modules: modules, hashes: a.Hashes})
			}
			if len(modules) == 0 {
				message := fmt.Sprintf("artifact %s has no server-visible %s modules", a.Filename, descriptorLoader)
				lastErr = errors.New(message)
				ledger.record(r.key, r.display, string(candidate.Version), message, RejectionModules)
				trace.rejected(r.display, string(candidate.Version), message)
				continue
			}
			a.Modules = modules
			requirements := r.requirements
			if replacingArtifactless {
				requirements = append(slices.Clone(artifactless.Requirements), r.requirements...)
			}
			var dependencyOf []catalog.Ref
			if dependencyDemand {
				dependencyOf = []catalog.Ref{r.dependencyOf}
			}
			pkg := document.Package{
				Environment:         r.key.environment,
				Ecosystem:           r.key.ecosystem,
				Artifact:            a,
				Requirements:        requirements,
				Dependency:          dependencyDemand,
				DependencyOf:        dependencyOf,
				Dependencies:        releaseDependencies(candidate.Dependencies),
				DependenciesUnknown: candidate.DependenciesUnknown,
				Channel:             candidate.Channel,
			}
			// Direct selections cannot be superseded. A duplicate between
			// two of them cannot be repaired by any remaining request.
			var claims []moduleKey
			if r.origin == originManifest {
				var duplicate error
				var other projectKey
				claims, other, duplicate = claimNativeModules(native, r.key, pkg, target)
				if duplicate != nil {
					if causes == nil {
						causes = make(map[projectKey]bool)
					}
					causes[other] = true
					lastErr = duplicate
					ledger.record(r.key, r.display, string(candidate.Version), duplicate.Error(), RejectionModules)
					trace.rejected(r.display, string(candidate.Version), duplicate.Error())
					logger.Debug("resolve: duplicate native module", "reason", duplicate)
					continue
				}
			}
			// A pure dependency demand binds to a selected package that
			// answers every module identity of the candidate. The candidate
			// installs only when that substitution leaves no settled graph.
			if dependencyDemand && len(requirements) == 0 {
				var providerKey projectKey
				var ok bool
				if r.key.environment == serverpkg.EnvironmentServer {
					other := state.selectedRuntimeProject(target, r.key)
					if other != r.key && coversModules(target, state.chosen[other], pkg, maps.Values(state.chosen)) {
						providerKey, ok = other, true
					}
				}
				if !ok && len(r.releaseHints) == 0 {
					providerKey, ok = state.substitute(target, pkg)
				}
				if ok {
					provider, _ := state.selection(providerKey)
					branch := state.extend(
						providerKey,
						withDependents(provider, dependencyOf),
						len(r.releaseHints) != 0,
					)
					logger.Debug(
						"resolve: dependency answered by provider",
						"provider", r.key.provider,
						"project", r.key.project,
						"answered-by", providerKey.project,
					)
					if outcome := descend(rest); outcome != branchFailed {
						return outcome
					}
					state.restore(branch)
					blockers = nil
					explained = false
				}
			}
			branches := []selectionBranch{
				state.install(
					r.key,
					pkg,
					conflictDependencies(candidate),
					len(r.releaseHints) > 0,
				),
			}
			branches = append(branches, state.supersede(target, r.key)...)
			logger.Debug(
				"resolve: candidate selected",
				"provider", r.key.provider,
				"project", r.key.project,
				"candidate", candidate.Version,
				"file", candidate.Filename,
				"modules", len(modules),
			)
			trace.selected(r.display, string(candidate.Version))
			next := rest
			if replayEdges {
				next = append(next, pinnedEdges[r.key]...)
			} else if expandDependencies {
				for _, dep := range candidate.Dependencies {
					if dep.Kind == catalog.DependencyKindRequired || (withOptional && dep.Kind == catalog.DependencyKindOptional) {
						var hints []releaseHint
						if dep.ReleaseID != "" {
							hints = append(hints, releaseHint{releaseID: dep.ReleaseID})
						}
						dependency := projectRequest{
							key: projectKey{
								r.key.environment,
								r.key.ecosystem,
								dep.Provider,
								dep.ProjectID,
							},
							origin:       originDependency,
							releaseHints: hints,
						}
						if dep.Kind == catalog.DependencyKindRequired {
							dependency.dependencyOf = catalog.Ref{
								Provider: r.key.provider,
								Name:     catalog.Name(r.key.project),
							}
						}
						if dep.Kind == catalog.DependencyKindOptional {
							dependency.requirements = []document.RequirementBinding{
								{
									Reference: dep.Provider.String() + ":" + dep.ProjectID,
									Selector:  versionpkg.Stable,
									Enabled:   true,
								},
							}
						}
						next = append(next, dependency)
					}
				}
			}
			if outcome := descend(next); outcome != branchFailed {
				return outcome
			}
			state.restoreAll(branches)
			for _, key := range claims {
				delete(native, key)
			}
			if blockers == nil {
				explained = false
			} else {
				if !blockers[r.key] {
					return branchFailed
				}
				delete(blockers, r.key)
				if causes == nil {
					causes = blockers
				} else {
					maps.Copy(causes, blockers)
				}
				blockers = nil
			}
			logger.Debug(
				"resolve: backtracking candidate",
				"provider", r.key.provider,
				"project", r.key.project,
				"candidate", candidate.Version,
				"reason", "nested resolution failed",
			)
			ledger.record(
				r.key,
				r.display,
				string(candidate.Version),
				"a required dependency could not be resolved",
				RejectionNested,
			)
		}
		giveUp()
		if !eligible && r.origin == originManifest {
			return searchStopped
		}
		if explained && causes != nil {
			blockers = causes
		}
		return branchFailed
	}
	search := func(allowBroken bool) searchOutcome {
		state.reset()
		attempts = 0
		backtracks = 0
		clear(native)
		blockers = nil
		strict = !allowBroken
		lastErr = nil
		operational = false
		ledger.reset()
		return backtrack(requests)
	}
	finalFailure := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if operational {
			var limit *SearchLimitError
			if errors.As(lastErr, &limit) {
				// The search stopped early, so keep the rejection ledger the
				// command layer renders instead of the bare stop message.
				summary := ledger.summary(target.Minecraft, limit)
				if summary == nil {
					summary = &Failure{Minecraft: target.Minecraft}
				}
				summary.Cause = limit
				summary.Aborted = true
				return summary
			}
			return lastErr
		}
		return resolutionFailure(ledger.summary(target.Minecraft, lastErr), lastErr)
	}
	// A stopped search cannot be relaxed: no candidate choice changes a
	// cancelled context, an exhausted budget, or a missing catalogue answer.
	outcome := search(false)
	if outcome == branchFailed && usedPin {
		logger.Debug("resolve: prior pins did not settle; considering catalogue alternatives", "reason", lastErr)
		preferPins = false
		outcome = search(false)
	}
	if outcome == branchFailed && acceptBroken {
		logger.Debug(
			"resolve: retrying with broken graphs",
			"reason", lastErr,
		)
		outcome = search(true)
	}
	if outcome != graphSettled {
		return nil, nil, finalFailure()
	}
	out := settled
	for i := range out {
		slices.SortFunc(out[i].DependencyOf, func(a, b catalog.Ref) int {
			return cmp.Or(cmp.Compare(a.Provider, b.Provider), cmp.Compare(a.Name, b.Name))
		})
	}
	var warnings []error
	if acceptPackageConflicts {
		warnings = append(warnings, packageConflictWarnings(out, state.conflicts)...)
	}
	if len(problems) > 0 {
		if acceptBroken {
			warnings = append(warnings, problemErrors(problems)...)
		} else if acceptPackageConflicts {
			// A requirement the server runtime answers is not something a
			// package choice can change, so it stays fatal even when package
			// conflicts are accepted.
			if fatal := serverModuleProblems(problems); len(fatal) > 0 {
				return nil, nil, errors.Join(problemErrors(fatal)...)
			}
			warnings = append(warnings, problemErrors(problems)...)
		} else {
			return nil, nil, errors.Join(problemErrors(problems)...)
		}
	}
	warnings = append(warnings, provenanceWarnings(out, provenance, target, expandDependencies)...)
	logger.Info(
		"resolve: selected packages",
		"packages", len(out),
	)
	return out, warnings, nil
}

// serverModuleProblems keeps the module problems the server runtime itself
// answers. No candidate choice can satisfy such a requirement, so accepting
// package conflicts never turns one into a warning.
func serverModuleProblems(problems []ModuleProblem) []ModuleProblem {
	var fatal []ModuleProblem
	for _, problem := range problems {
		if problem.Server {
			fatal = append(fatal, problem)
		}
	}
	return fatal
}

func mergeArtifactSelections(packages []document.Package) []document.Package {
	type key struct {
		environment            serverpkg.Environment
		ecosystem              catalog.Ecosystem
		provider               catalog.Provider
		project, release, file string
	}
	index := map[key]int{}
	out := make([]document.Package, 0, len(packages))
	for _, p := range packages {
		k := key{
			p.Environment,
			p.Ecosystem,
			p.Provider,
			p.ProjectID,
			p.ReleaseID,
			p.FileID,
		}
		if i, ok := index[k]; ok {
			out[i].Requirements = append(out[i].Requirements, p.Requirements...)
			out[i].Dependency = out[i].Dependency || p.Dependency
			for _, source := range p.DependencyOf {
				if !slices.Contains(out[i].DependencyOf, source) {
					out[i].DependencyOf = append(out[i].DependencyOf, source)
				}
			}
			continue
		}
		index[k] = len(out)
		out = append(out, p)
	}
	return out
}

// sortPackages orders packages by their persisted identity so validation
// reports findings in a stable order.
func sortPackages(packages []document.Package) {
	sort.Slice(packages, func(i, j int) bool {
		a, b := &packages[i], &packages[j]
		if n := strings.Compare(string(a.Environment), string(b.Environment)); n != 0 {
			return n < 0
		}
		if n := strings.Compare(string(a.Ecosystem), string(b.Ecosystem)); n != 0 {
			return n < 0
		}
		if n := strings.Compare(string(a.Provider), string(b.Provider)); n != 0 {
			return n < 0
		}
		return strings.Compare(a.ProjectID, b.ProjectID) < 0
	})
}
