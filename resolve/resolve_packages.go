package resolve

import (
	"cmp"
	"context"
	"errors"
	"fmt"
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

// searchAttemptLimit bounds how many candidate combinations one resolution
// may explore before it reports the stop together with the rejection ledger.
const searchAttemptLimit = 10000

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

// lockedFileID returns the file the lock selected for a reference. A release
// may publish several files of one version, and the recorded variant must stay
// eligible so a later resolution does not swap in the release's primary file.
func lockedFileID(
	previous []document.Package,
	environment serverpkg.Environment,
	ecosystem catalog.Ecosystem,
	provider catalog.Provider,
	ref string,
) string {
	for _, p := range previous {
		if p.Environment != environment || p.Ecosystem != ecosystem ||
			p.Provider != provider {
			continue
		}
		for _, requirement := range p.Requirements {
			if requirement.Reference == ref {
				return p.FileID
			}
		}
	}
	return ""
}

// lockedCandidate rebuilds the candidate the lock accepted for this request.
// An adopted release can carry a compatibility exception that ordinary
// provider filtering drops, so the locked file stays selectable while its
// runtime target is unchanged. The candidate keeps the dependency metadata the
// lock recorded, because the catalogue no longer supplies it.
func lockedCandidate(
	previous []document.Package,
	r projectRequest,
) (upstream.Candidate, bool) {
	for _, p := range previous {
		if p.Environment != r.key.environment ||
			p.Ecosystem != r.key.ecosystem ||
			p.Provider != r.key.provider ||
			p.ProjectID != r.key.project ||
			p.Filename == "" {
			continue
		}
		locked := upstream.Candidate{Artifact: p.Artifact}
		locked.PrimaryFile = true
		locked.Channel = p.Channel
		locked.CompatibilityUnknown = true
		locked.Dependencies = candidateDependencies(p.Dependencies)
		locked.DependenciesUnknown = p.DependenciesUnknown
		return locked, true
	}
	return upstream.Candidate{}, false
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
	previous []document.Package,
	r projectRequest,
) []upstream.Candidate {
	locked, ok := lockedCandidate(previous, r)
	if !ok {
		return list
	}
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
	previous := request.Previous
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
		for _, p := range previous {
			if p.Environment == environment && p.Ecosystem == ecosystem && p.Provider == provider {
				for _, old := range p.Requirements {
					if old.Reference == ref {
						project = p.ProjectID
					}
				}
			}
		}
		requests = append(requests, projectRequest{
			key: projectKey{
				environment,
				ecosystem,
				provider,
				project,
			},
			display: display,
			origin:  originManifest,
			previousFileID: lockedFileID(
				previous,
				environment,
				ecosystem,
				provider,
				ref,
			),
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
	attempts := 0
	strict := true
	var lastErr error
	ledger := newRejectionLedger()
	logger.Info(
		"resolve: resolving package requirements",
		"requests", len(requests),
	)
	// A hard failure is an error that no candidate rejection explains, such as
	// a network, checksum, or archive error. It outranks the rejection summary.
	operational := false
	hardFail := func(err error) bool {
		lastErr = err
		operational = true
		return false
	}
	var backtrack func([]projectRequest) bool
	backtrack = func(pending []projectRequest) bool {
		if err := ctx.Err(); err != nil {
			return hardFail(err)
		}
		attempts++
		if attempts > searchAttemptLimit {
			return hardFail(&SearchLimitError{Attempts: searchAttemptLimit})
		}
		trace.state(len(state.chosen), len(pending))
		if len(pending) == 0 {
			all := make([]document.Package, 0, len(state.chosen))
			for _, p := range state.chosen {
				all = append(all, p)
			}
			all = mergeArtifactSelections(all)
			sortPackages(all)
			problems, err := finalizeModules(all, target)
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
					return true
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
				return false
			}
			return true
		}
		r := pending[0]
		rest := pending[1:]
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
			if backtrack(rest) {
				return true
			}
			state.restore(branch)
			logger.Debug(
				"resolve: backtracking requirement merge",
				"provider", r.key.provider,
				"project", r.key.project,
				"reason", "remaining requests failed after merge",
			)
			return false
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
			if backtrack(rest) {
				return true
			}
			state.restore(branch)
			return false
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
		if !ok {
			if candidatesFor == nil {
				return hardFail(errors.New("resolution requires a candidate source"))
			}
			var err error
			list, err = candidatesFor(ctx, catalogueRegistry(r.key.provider), r.key.project, string(target.Minecraft), r.key.ecosystem)
			if err != nil {
				if !preserveLocked {
					return hardFail(err)
				}
				locked, ok := lockedCandidate(previous, r)
				if !ok {
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
				list = appendLockedCandidate(list, previous, r)
			}
			list = orderCandidates(list, previous, r)
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
				return backtrack(append([]projectRequest{{
					key:            canonicalKey,
					display:        r.display,
					origin:         r.origin,
					requirements:   r.requirements,
					dependencyOf:   r.dependencyOf,
					previousFileID: r.previousFileID,
					releaseHints:   r.releaseHints,
				}}, rest...))
			}
		}
		if len(list) == 0 {
			ledger.record(r.key, r.display, "", "no published builds match this server", RejectionEmpty)
		}
		list = preferReleaseBuilds(list, r.requirements)
		list = preferReleaseHints(list, r.releaseHints)
		for _, candidate := range list {
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
			if detail, conflicting := metadataConflict(candidate, r, state.chosen, state.conflicts); conflicting {
				if !acceptPackageConflicts {
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
			entry, cached := analysisEntry{}, false
			if cache != nil {
				entry, cached = cache.get(candidateAnalysisKey(candidate))
			}
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
					return hardFail(err)
				}
				if cache != nil && len(modules) > 0 {
					cache.put(candidateAnalysisKey(candidate), analysisEntry{
						modules: modules,
						hashes:  a.Hashes,
					})
				}
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
			// A pure dependency demand binds to a selected package that
			// answers every module identity of the candidate. The candidate
			// installs only when that substitution leaves no settled graph.
			if dependencyDemand && len(requirements) == 0 && len(r.releaseHints) == 0 {
				if providerKey, ok := state.substitute(target, pkg); ok {
					provider, _ := state.selection(providerKey)
					branch := state.extend(
						providerKey,
						withDependents(provider, dependencyOf),
						false,
					)
					logger.Debug(
						"resolve: dependency answered by provider",
						"provider", r.key.provider,
						"project", r.key.project,
						"answered-by", providerKey.project,
					)
					if backtrack(rest) {
						return true
					}
					state.restore(branch)
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
			next := append([]projectRequest{}, rest...)
			if expandDependencies {
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
			if backtrack(next) {
				return true
			}
			state.restoreAll(branches)
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
		if lastErr == nil {
			lastErr = fmt.Errorf("no candidate satisfies %s:%s", r.key.provider, r.key.project)
		}
		ledger.markFailed(r.key)
		return false
	}
	search := func(allowBroken bool) bool {
		state.reset()
		attempts = 0
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
	if !search(false) {
		if !acceptBroken {
			return nil, nil, finalFailure()
		}
		logger.Debug(
			"resolve: retrying with broken graphs",
			"reason", lastErr,
		)
		if !search(true) {
			return nil, nil, finalFailure()
		}
	}
	var out []document.Package
	for _, p := range state.chosen {
		// A requirement the manifest disables and nothing demands pins no
		// artifact, so recording it would leave the lock claiming a package
		// that has no file to install.
		if p.Artifact.Filename == "" {
			continue
		}
		out = append(out, p)
	}
	out = mergeArtifactSelections(out)
	sortPackages(out)
	for i := range out {
		slices.SortFunc(out[i].DependencyOf, func(a, b catalog.Ref) int {
			return cmp.Or(cmp.Compare(a.Provider, b.Provider), cmp.Compare(a.Name, b.Name))
		})
	}
	problems, err := finalizeModules(out, target)
	if err != nil {
		return nil, nil, err
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
