package resolve

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
	serverpkg "github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/upstream/routing"

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

func Packages(request PackagesRequest) ([]lockfile.Package, []error, error) {
	ctx := request.Context
	if ctx == nil {
		ctx = context.Background()
	}
	d := request.Manifest
	previous := request.Previous
	target := request.Target
	materialize := request.Materialize
	withOptional := request.WithOptional
	acceptBroken := request.AcceptBroken
	expandDependencies := request.ExpandDependencies
	trace := request.Trace
	var requests []projectRequest
	appendRequest := func(environment serverpkg.Environment, ecosystem catalog.Ecosystem, ref string, r manifest.Requirement) error {
		provider, project, err := manifest.ParseReference(ref)
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
			requirements: []lockfile.RequirementBinding{
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
	canonical := map[projectKey]projectKey{}
	chosen := map[projectKey]lockfile.Package{}
	conflicts := map[projectKey][]upstream.ProjectDependency{}
	cache := request.Cache
	attempts := 0
	strict := true
	var lastErr error
	ledger := newRejectionLedger()
	log.Info(
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
		trace.state(len(chosen), len(pending))
		if len(pending) == 0 {
			all := make([]lockfile.Package, 0, len(chosen))
			for _, p := range chosen {
				all = append(all, p)
			}
			all = mergeArtifactSelections(all)
			sortPackages(all)
			problems, err := finalizeModules(all, target)
			if err != nil {
				log.Debug(
					"resolve: settled graph invalid",
					"attempt", attempts,
					"reason", err,
				)
				return hardFail(err)
			}
			if len(problems) > 0 && strict {
				log.Debug(
					"resolve: settled graph rejected",
					"attempt", attempts,
					"reason", problems[0],
				)
				lastErr = errors.Join(problemErrors(problems)...)
				for _, problem := range problems {
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
					if owner, ok := chosen[key]; ok {
						version = string(owner.Version)
					}
					ledger.record(
						key,
						display,
						version,
						problem.Message,
						RejectionModules,
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
		dependencyDemand := len(r.requirements) == 0
		active := dependencyDemand
		for _, req := range r.requirements {
			active = active || req.Enabled
		}
		// An artifact-less entry records only a disabled direct binding. When
		// something else in the graph needs the package, that demand outranks
		// the binding, so the artifact resolves instead of merging into a
		// package that no file can satisfy.
		if previous, ok := chosen[r.key]; ok && (!active || previous.Artifact.Filename != "") {
			before := previous.Requirements
			wasDependency := previous.Dependency
			previous.Requirements = append(previous.Requirements, r.requirements...)
			previous.Dependency = previous.Dependency || dependencyDemand
			chosen[r.key] = previous
			if backtrack(rest) {
				return true
			}
			previous.Requirements = before
			previous.Dependency = wasDependency
			chosen[r.key] = previous
			log.Debug(
				"resolve: backtracking requirement merge",
				"provider", r.key.provider,
				"project", r.key.project,
				"reason", "remaining requests failed after merge",
			)
			return false
		}
		if !active {
			p := lockfile.Package{
				Environment:  r.key.environment,
				Ecosystem:    r.key.ecosystem,
				Provider:     r.key.provider,
				ProjectID:    r.key.project,
				Requirements: r.requirements,
			}
			chosen[r.key] = p
			if backtrack(rest) {
				return true
			}
			delete(chosen, r.key)
			delete(conflicts, r.key)
			return false
		}
		// A disabled direct binding may already hold an artifact-less entry
		// here, because a real demand outranks it. Selecting a file replaces
		// the entry, so the binding's requirements have to travel into the
		// selection and back out again when the selection does not resolve.
		var artifactless lockfile.Package
		replacingArtifactless := false
		if prior, ok := chosen[r.key]; ok && prior.Artifact.Filename == "" {
			artifactless, replacingArtifactless = prior, true
		}
		list, ok := candidates[r.key]
		if !ok {
			var err error
			list, err = routing.Candidates(ctx, catalogueRegistry(r.key.provider), r.key.project, string(target.Minecraft), r.key.ecosystem)
			if err != nil {
				return hardFail(err)
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
				log.Debug(
					"resolve: project identity canonicalized",
					"provider", r.key.provider,
					"reference", r.key.project,
					"project", project,
				)
				return backtrack(append([]projectRequest{{
					key:          canonicalKey,
					display:      r.display,
					origin:       r.origin,
					requirements: r.requirements,
					releaseHints: r.releaseHints,
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
				log.Debug(
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
			if detail, conflicting := metadataConflict(candidate, r, chosen, conflicts); conflicting {
				reason := describeRejection(RejectionMetadataConflict, detail)
				ledger.record(r.key, r.display, string(candidate.Version), detail, RejectionMetadataConflict)
				trace.rejected(r.display, string(candidate.Version), reason)
				log.Debug(
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
			a := candidate.Artifact
			var modules []catalog.DescriptorModule
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
				modules, err = artifact.ReadModules(ctx, materialized.Path, r.key.ecosystem)
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
				message := fmt.Sprintf("artifact %s has no server-visible %s modules", a.Filename, r.key.ecosystem)
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
			chosen[r.key] = lockfile.Package{
				Environment:  r.key.environment,
				Ecosystem:    r.key.ecosystem,
				Artifact:     a,
				Requirements: requirements,
				Dependency:   dependencyDemand,
			}
			if declared := conflictDependencies(candidate); len(declared) > 0 {
				conflicts[r.key] = declared
			} else {
				delete(conflicts, r.key)
			}
			log.Debug(
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
						if dep.Kind == catalog.DependencyKindOptional {
							dependency.requirements = []lockfile.RequirementBinding{
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
			if replacingArtifactless {
				chosen[r.key] = artifactless
			} else {
				delete(chosen, r.key)
			}
			delete(conflicts, r.key)
			log.Debug(
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
		chosen = map[projectKey]lockfile.Package{}
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
		log.Debug(
			"resolve: retrying with broken graphs",
			"reason", lastErr,
		)
		if !search(true) {
			return nil, nil, finalFailure()
		}
	}
	var out []lockfile.Package
	for _, p := range chosen {
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
	problems, err := finalizeModules(out, target)
	if err != nil {
		return nil, nil, err
	}
	var warnings []error
	if len(problems) > 0 {
		if !acceptBroken {
			return nil, nil, errors.Join(problemErrors(problems)...)
		}
		warnings = problemErrors(problems)
	}
	log.Info(
		"resolve: selected packages",
		"packages", len(out),
	)
	return out, warnings, nil
}

func mergeArtifactSelections(packages []lockfile.Package) []lockfile.Package {
	type key struct {
		environment            serverpkg.Environment
		ecosystem              catalog.Ecosystem
		provider               catalog.Provider
		project, release, file string
	}
	index := map[key]int{}
	out := make([]lockfile.Package, 0, len(packages))
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
			continue
		}
		index[k] = len(out)
		out = append(out, p)
	}
	return out
}

// sortPackages orders packages by their persisted identity so validation
// reports findings in a stable order.
func sortPackages(packages []lockfile.Package) {
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
