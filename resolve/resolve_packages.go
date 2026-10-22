package resolve

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
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

type (
	projectKey struct {
		environment serverpkg.Environment
		ecosystem   catalog.Ecosystem
		provider    catalog.Provider
		project     string
	}
	projectRequest struct {
		key          projectKey
		display      string
		requirements []lockfile.RequirementBinding
		releaseIDs   []string
	}
)

// catalogueRegistry names the registry that serves a package provider. Only a
// provider backing a registry can answer catalogue queries.
func catalogueRegistry(provider catalog.Provider) catalog.Registry {
	registry, ok := provider.Registry()
	if !ok {
		return catalog.RegistryUnspecified
	}
	return registry
}

// SupportsPackages reports whether the server product loads packages from
// ecosystem. A paperclip server loads Paper and Bukkit packages regardless of
// how the manifest names its product.
func SupportsPackages(server lockfile.Server, ecosystem catalog.Ecosystem) bool {
	switch server.Product {
	case serverpkg.ProductFabric:
		return ecosystem == catalog.EcosystemFabric
	case serverpkg.ProductForge:
		return ecosystem == catalog.EcosystemForge
	case serverpkg.ProductNeoForge:
		return ecosystem == catalog.EcosystemNeoForge
	case serverpkg.ProductPaper, serverpkg.ProductPurpur:
		return ecosystem == catalog.EcosystemPaper || ecosystem == catalog.EcosystemBukkit
	default:
		return server.Bootstrapper == "paperclip" &&
			(ecosystem == catalog.EcosystemPaper || ecosystem == catalog.EcosystemBukkit)
	}
}

// PackagesRequest describes one package resolution. Trace and Context
// default to their zero values, which report nothing and never cancel.
type PackagesRequest struct {
	Context            context.Context
	Manifest           *manifest.Document
	Previous           *lockfile.Document
	Server             lockfile.Server
	MCDRVersion        string
	Materialize        func(*catalog.Artifact) (string, error)
	WithOptional       bool
	AcceptBroken       bool
	ExpandDependencies bool
	Trace              Trace
}

func Packages(request PackagesRequest) ([]lockfile.Package, []error, error) {
	ctx := request.Context
	if ctx == nil {
		ctx = context.Background()
	}
	d := request.Manifest
	previous := request.Previous
	server := request.Server
	mcdrVersion := request.MCDRVersion
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
		if previous != nil {
			for _, p := range previous.Packages {
				if p.Environment == environment && p.Ecosystem == ecosystem && p.Provider == provider {
					for _, old := range p.Requirements {
						if old.Reference == ref {
							project = p.ProjectID
						}
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
			if !SupportsPackages(server, r.key.ecosystem) {
				for _, req := range r.requirements {
					if req.Enabled {
						return nil, nil, fmt.Errorf("product %s cannot load %s packages", server.Product, r.key.ecosystem)
					}
				}
			}
		}
	}
	candidates := map[projectKey][]upstream.Candidate{}
	canonical := map[projectKey]projectKey{}
	chosen := map[projectKey]lockfile.Package{}
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
		if attempts > 10000 {
			return hardFail(fmt.Errorf("package resolution exceeded 10000 candidate combinations"))
		}
		trace.state(len(chosen), len(pending))
		if len(pending) == 0 {
			all := make([]lockfile.Package, 0, len(chosen))
			for _, p := range chosen {
				all = append(all, p)
			}
			all = mergeArtifactSelections(all)
			sortPackages(all)
			problems, err := ValidateModules(all, server, mcdrVersion)
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
		active := len(r.requirements) == 0
		for _, req := range r.requirements {
			active = active || req.Enabled
		}
		// An artifact-less entry records only a disabled direct binding. When
		// something else in the graph needs the package, that demand outranks
		// the binding, so the artifact resolves instead of merging into a
		// package that no file can satisfy.
		if previous, ok := chosen[r.key]; ok && (!active || previous.Artifact.Filename != "") {
			for _, release := range r.releaseIDs {
				if release != "" && previous.ReleaseID != release {
					return hardFail(fmt.Errorf("conflicting exact releases for %s:%s", r.key.provider, r.key.project))
				}
			}
			before := previous.Requirements
			previous.Requirements = append(previous.Requirements, r.requirements...)
			chosen[r.key] = previous
			if backtrack(rest) {
				return true
			}
			previous.Requirements = before
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
			list, err = routing.Candidates(ctx, catalogueRegistry(r.key.provider), r.key.project, string(server.Minecraft), r.key.ecosystem)
			if err != nil {
				return hardFail(err)
			}
			slices.SortStableFunc(list, func(a, b upstream.Candidate) int { return b.PublishedAt.Compare(a.PublishedAt) })
			if previous != nil {
				for _, p := range previous.Packages {
					if p.Environment == r.key.environment && p.Ecosystem == r.key.ecosystem && p.Provider == r.key.provider && p.ProjectID == r.key.project {
						for i, c := range list {
							if c.ReleaseID == p.ReleaseID && c.FileID == p.FileID {
								list = append([]upstream.Candidate{c}, append(list[:i], list[i+1:]...)...)
								break
							}
						}
					}
				}
			}
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
					requirements: r.requirements,
					releaseIDs:   r.releaseIDs,
				}}, rest...))
			}
		}
		if len(list) == 0 {
			ledger.record(r.key, r.display, "", "no published builds match this server", RejectionEmpty)
		}
		list = preferReleaseBuilds(list, r.requirements)
		for _, candidate := range list {
			eligible := true
			reason := ""
			var rejectKind RejectionKind
			var rejectDetail string
			reject := func(kind RejectionKind, detail string) {
				eligible = false
				if reason == "" {
					reason = describeRejection(kind, detail)
					rejectKind = kind
					rejectDetail = detail
				}
			}
			for _, release := range r.releaseIDs {
				if release != "" && candidate.ReleaseID != release {
					reject(RejectionReleaseMismatch, release)
				}
			}
			constrained := false
			for _, req := range r.requirements {
				if !req.Enabled {
					continue
				}
				constrained = true
				switch req.Selector {
				case versionpkg.Stable:
					if candidate.Channel != upstream.ReleaseChannelRelease {
						reject(RejectionChannel, "requested a release build")
					}
				case versionpkg.Any, versionpkg.Beta, "":
				default:
					if candidate.Version != req.Selector && candidate.ReleaseID != string(req.Selector) && candidate.FileID != string(req.Selector) && candidate.Filename != string(req.Selector) {
						reject(RejectionVersionMismatch, string(req.Selector))
					}
				}
				if req.FilePattern != "" {
					match, err := filepath.Match(req.FilePattern, candidate.Filename)
					if err != nil {
						return hardFail(err)
					}
					if !match {
						reject(RejectionFilePattern, req.FilePattern)
					}
				} else if !candidate.PrimaryFile {
					reject(RejectionPrimaryFile, "")
				}
			}
			// A dependency request carries no requirement of its own, so no
			// enabled binding rejects the extra files a release publishes: the
			// sources and dev jars carry a readable descriptor and would
			// otherwise win the version race on publication date alone.
			if !constrained && !candidate.PrimaryFile {
				reject(RejectionPrimaryFile, "")
			}
			if !eligible {
				ledger.record(r.key, r.display, string(candidate.Version), rejectDetail, rejectKind)
				trace.rejected(r.display, string(candidate.Version), reason)
				log.Debug(
					"resolve: candidate rejected",
					"provider", r.key.provider,
					"project", r.key.project,
					"candidate", candidate.Version,
					"file", candidate.Filename,
					"reason", reason,
				)
				continue
			}
			a := candidate.Artifact
			p, err := materialize(&a)
			if err != nil {
				return hardFail(err)
			}
			modules, err := artifact.ReadModules(ctx, p, r.key.ecosystem)
			if err != nil {
				return hardFail(err)
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
						var ids []string
						if dep.ReleaseID != "" {
							ids = append(ids, dep.ReleaseID)
						}
						dependency := projectRequest{
							key: projectKey{
								r.key.environment,
								r.key.ecosystem,
								dep.Provider,
								dep.ProjectID,
							}, releaseIDs: ids,
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
			return lastErr
		}
		return resolutionFailure(ledger.summary(server.Minecraft, lastErr), lastErr)
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
	problems, err := ValidateModules(out, server, mcdrVersion)
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

// preferReleaseBuilds applies the default selector's preference: `any` takes
// the newest build but prefers a release over a prerelease when both exist.
// `beta` keeps publication order because it accepts prereleases outright, and
// `stable` rejects prereleases in the candidate loop.
func preferReleaseBuilds(list []upstream.Candidate, requirements []lockfile.RequirementBinding) []upstream.Candidate {
	prefer := false
	for _, req := range requirements {
		if !req.Enabled {
			continue
		}
		if req.Selector == versionpkg.Any || req.Selector == "" {
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

func ValidateModules(packages []lockfile.Package, server lockfile.Server, mcdrVersion string) ([]ModuleProblem, error) {
	type selected struct{ packageIndex, moduleIndex int }
	index := map[struct {
		environment serverpkg.Environment
		ecosystem   catalog.Ecosystem
		id          string
	}]selected{}
	var problems []ModuleProblem
	for pi := range packages {
		p := &packages[pi]
		if !p.Enabled() {
			continue
		}
		for mi, m := range p.Modules {
			names := append([]string{m.ID}, m.Provides...)
			for _, name := range names {
				if m.Ecosystem == catalog.EcosystemBukkit || m.Ecosystem == catalog.EcosystemPaper {
					name = strings.ToLower(name)
				}
				key := struct {
					environment serverpkg.Environment
					ecosystem   catalog.Ecosystem
					id          string
				}{p.Environment, p.Ecosystem, name}
				if old, ok := index[key]; ok {
					previous := packages[old.packageIndex].Modules[old.moduleIndex]
					if m.NestedPath == "" && previous.NestedPath == "" && pi != old.packageIndex {
						return nil, fmt.Errorf("duplicate native module %s in %s and %s", name, p.Filename, packages[old.packageIndex].Filename)
					}
					if previous.NestedPath == "" {
						continue
					}
					if m.NestedPath != "" {
						a, err := versionpkg.ParseVersion(
							previous.Version,
							previous.EffectiveVersionDialect(),
						)
						if err != nil {
							return nil, err
						}
						b, err := versionpkg.ParseVersion(
							m.Version,
							m.EffectiveVersionDialect(),
						)
						if err != nil {
							return nil, err
						}
						comparison, isComparable := versionpkg.Compare(a, b)
						if !isComparable {
							return nil, fmt.Errorf("incomparable nested module versions %s and %s", previous.Version, m.Version)
						}
						if comparison >= 0 {
							continue
						}
					}
				}
				index[key] = selected{pi, mi}
			}
		}
	}
	for pi := range packages {
		p := &packages[pi]
		if !p.Enabled() {
			continue
		}
		var selectedModules []catalog.DescriptorModule
		for mi := range p.Modules {
			m := p.Modules[mi]
			id := m.ID
			if m.Ecosystem == catalog.EcosystemBukkit || m.Ecosystem == catalog.EcosystemPaper {
				id = strings.ToLower(id)
			}
			key := struct {
				environment serverpkg.Environment
				ecosystem   catalog.Ecosystem
				id          string
			}{p.Environment, p.Ecosystem, id}
			if target := index[key]; target.packageIndex != pi || target.moduleIndex != mi {
				continue
			}
			for di := range m.Dependencies {
				dep := &m.Dependencies[di]
				// A Java requirement is checked against the Java runtime that
				// runs the server, and only warns.
				if dep.Kind == catalog.DependencyKindPython || dep.ID == "java" {
					continue
				}
				value := ""
				builtin := true
				switch dep.ID {
				case "minecraft":
					value = string(server.Minecraft)
				case "fabricloader":
					if server.Product == serverpkg.ProductFabric {
						value = string(server.CoreVersion)
					}
				case "forge", "neoforge":
					if dep.ID == server.Product.String() {
						value = string(server.CoreVersion)
					}
				case "mcdreforged":
					value = mcdrVersion
				default:
					builtin = false
				}
				var target *lockfile.Package
				var targetModule *catalog.DescriptorModule
				if !builtin {
					name := dep.ID
					if p.Ecosystem == catalog.EcosystemBukkit || p.Ecosystem == catalog.EcosystemPaper {
						name = strings.ToLower(name)
					}
					if match, ok := index[struct {
						environment serverpkg.Environment
						ecosystem   catalog.Ecosystem
						id          string
					}{p.Environment, p.Ecosystem, name}]; ok {
						target = &packages[match.packageIndex]
						targetModule = &target.Modules[match.moduleIndex]
						value = string(targetModule.Version)
					}
				}
				matched := value != ""
				invalid := false
				if matched && dep.Constraint != "" {
					var err error
					matched, err = versionpkg.Match(
						dep.Constraint,
						versionpkg.Version(value),
						m.EffectiveVersionDialect(),
					)
					if err != nil {
						problems = append(problems, moduleProblem(p, fmt.Sprintf("module %s requires %s %s: %v", m.ID, dep.ID, dep.Constraint, err)))
						invalid = true
					}
				}
				requirement := strings.TrimSpace(dep.ID + " " + dep.Constraint)
				if !invalid && dep.Kind == catalog.DependencyKindRequired && !matched {
					if value == "" {
						problems = append(problems, moduleProblem(p, fmt.Sprintf("module %s requires %s, which no selected package provides", m.ID, requirement)))
					} else {
						problems = append(problems, moduleProblem(p, fmt.Sprintf("module %s requires %s (selected %s)", m.ID, requirement, value)))
					}
				}
				if !invalid && dep.Kind == catalog.DependencyKindIncompatible && matched {
					problems = append(problems, moduleProblem(p, fmt.Sprintf("module %s breaks %s %s", m.ID, dep.ID, value)))
				}
				if matched {
					if target != nil {
						dep.Provider = target.Provider
						dep.ProjectID = target.ProjectID
						dep.ModuleID = targetModule.ID
					} else {
						dep.ModuleID = dep.ID
					}
				}
			}
			selectedModules = append(selectedModules, m)
		}
		p.Modules = selectedModules
	}
	return problems, nil
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
	slices.SortFunc(packages, func(a, b lockfile.Package) int {
		if n := strings.Compare(string(a.Environment), string(b.Environment)); n != 0 {
			return n
		}
		if n := strings.Compare(string(a.Ecosystem), string(b.Ecosystem)); n != 0 {
			return n
		}
		if n := strings.Compare(string(a.Provider), string(b.Provider)); n != 0 {
			return n
		}
		return strings.Compare(a.ProjectID, b.ProjectID)
	})
}
