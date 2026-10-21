package resolve

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/manifest"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream/routing"
	"github.com/mclucy/lucy/version"
)

type (
	projectKey struct {
		environment types.Environment
		ecosystem   types.Ecosystem
		provider    types.Provider
		project     string
	}
	projectRequest struct {
		key          projectKey
		requirements []types.RequirementBinding
		releaseIDs   []string
	}
)

// catalogueRegistry names the registry that serves a package provider. Only a
// provider backing a registry can answer catalogue queries.
func catalogueRegistry(provider types.Provider) types.Registry {
	registry, ok := provider.Registry()
	if !ok {
		return types.RegistryUnspecified
	}
	return registry
}

func Packages(ctx context.Context, d *manifest.Document, previous *lockfile.Document, server lockfile.Server, mcdrVersion string, materialize func(*types.Artifact) (string, error), withOptional bool, acceptBroken bool) ([]types.LockedPackage, []error, error) {
	var requests []projectRequest
	appendRequest := func(environment types.Environment, ecosystem types.Ecosystem, ref string, r manifest.Requirement) error {
		provider, project, err := manifest.ParseReference(ref)
		if err != nil {
			return err
		}
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
			requirements: []types.RequirementBinding{
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
	var ecosystems []types.Ecosystem
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
			if err := appendRequest(types.EnvironmentServer, ecosystem, ref, d.Server.Packages[ecosystem][ref]); err != nil {
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
			if err := appendRequest(types.EnvironmentMCDR, types.EcosystemMCDR, ref, d.MCDR.Packages[ref]); err != nil {
				return nil, nil, err
			}
		}
	}
	for _, r := range requests {
		if r.key.environment == types.EnvironmentServer {
			supported := false
			switch server.Product {
			case types.ServerProductFabric:
				supported = r.key.ecosystem == types.EcosystemFabric
			case types.ServerProductForge:
				supported = r.key.ecosystem == types.EcosystemForge
			case types.ServerProductNeoForge:
				supported = r.key.ecosystem == types.EcosystemNeoForge
			case types.ServerProductPaper, types.ServerProductPurpur:
				supported = r.key.ecosystem == types.EcosystemPaper ||
					r.key.ecosystem == types.EcosystemBukkit
			default:
				supported = server.Bootstrapper == "paperclip" &&
					(r.key.ecosystem == types.EcosystemPaper ||
						r.key.ecosystem == types.EcosystemBukkit)
			}
			if !supported {
				for _, req := range r.requirements {
					if req.Enabled {
						return nil, nil, fmt.Errorf("product %s cannot load %s packages", server.Product, r.key.ecosystem)
					}
				}
			}
		}
	}
	candidates := map[projectKey][]types.CatalogueCandidate{}
	canonical := map[projectKey]projectKey{}
	chosen := map[projectKey]types.LockedPackage{}
	attempts := 0
	strict := true
	var lastErr error
	log.Info(
		"resolve: resolving package requirements",
		"requests", len(requests),
	)
	var backtrack func([]projectRequest) bool
	backtrack = func(pending []projectRequest) bool {
		if err := ctx.Err(); err != nil {
			lastErr = err
			return false
		}
		attempts++
		if attempts > 10000 {
			lastErr = fmt.Errorf("package resolution exceeded 10000 candidate combinations")
			return false
		}
		if len(pending) == 0 {
			all := make([]types.LockedPackage, 0, len(chosen))
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
				lastErr = err
				return false
			}
			if len(problems) > 0 && strict {
				log.Debug(
					"resolve: settled graph rejected",
					"attempt", attempts,
					"reason", problems[0],
				)
				lastErr = errors.Join(problems...)
				return false
			}
			return true
		}
		r := pending[0]
		rest := pending[1:]
		if canonicalKey, ok := canonical[r.key]; ok {
			r.key = canonicalKey
		}
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
					lastErr = fmt.Errorf("conflicting exact releases for %s:%s", r.key.provider, r.key.project)
					return false
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
			p := types.LockedPackage{
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
		var artifactless types.LockedPackage
		replacingArtifactless := false
		if prior, ok := chosen[r.key]; ok && prior.Artifact.Filename == "" {
			artifactless, replacingArtifactless = prior, true
		}
		list, ok := candidates[r.key]
		if !ok {
			var err error
			list, err = routing.Candidates(ctx, catalogueRegistry(r.key.provider), r.key.project, string(server.Minecraft), r.key.ecosystem)
			if err != nil {
				lastErr = err
				return false
			}
			slices.SortStableFunc(list, func(a, b types.CatalogueCandidate) int { return b.PublishedAt.Compare(a.PublishedAt) })
			if previous != nil {
				for _, p := range previous.Packages {
					if p.Environment == r.key.environment && p.Ecosystem == r.key.ecosystem && p.Provider == r.key.provider && p.ProjectID == r.key.project {
						for i, c := range list {
							if c.ReleaseID == p.ReleaseID && c.FileID == p.FileID {
								list = append([]types.CatalogueCandidate{c}, append(list[:i], list[i+1:]...)...)
								break
							}
						}
					}
				}
			}
			candidates[r.key] = list
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
				log.Debug(
					"resolve: project identity canonicalized",
					"provider", r.key.provider,
					"reference", r.key.project,
					"project", project,
				)
				return backtrack(append([]projectRequest{{
					key:          canonicalKey,
					requirements: r.requirements,
					releaseIDs:   r.releaseIDs,
				}}, rest...))
			}
		}
		list = preferReleaseBuilds(list, r.requirements)
		for _, candidate := range list {
			eligible := true
			reason := ""
			reject := func(cause string) {
				eligible = false
				if reason == "" {
					reason = cause
				}
			}
			for _, release := range r.releaseIDs {
				if release != "" && candidate.ReleaseID != release {
					reject("release mismatch")
				}
			}
			constrained := false
			for _, req := range r.requirements {
				if !req.Enabled {
					continue
				}
				constrained = true
				switch req.Selector {
				case types.VersionStable:
					if candidate.Channel != types.ReleaseChannelRelease {
						reject("not a release build")
					}
				case types.VersionAny, types.VersionBeta, "":
				default:
					if candidate.Version != req.Selector && candidate.ReleaseID != string(req.Selector) && candidate.FileID != string(req.Selector) && candidate.Filename != string(req.Selector) {
						reject("selector mismatch")
					}
				}
				if req.FilePattern != "" {
					match, err := filepath.Match(req.FilePattern, candidate.Filename)
					if err != nil {
						lastErr = err
						return false
					}
					if !match {
						reject("file pattern mismatch")
					}
				} else if !candidate.PrimaryFile {
					reject("not the primary file")
				}
			}
			// A dependency request carries no requirement of its own, so no
			// enabled binding rejects the extra files a release publishes: the
			// sources and dev jars carry a readable descriptor and would
			// otherwise win the version race on publication date alone.
			if !constrained && !candidate.PrimaryFile {
				reject("not the primary file")
			}
			if !eligible {
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
				lastErr = err
				return false
			}
			modules, err := artifact.ReadModules(ctx, p, r.key.ecosystem)
			if err != nil {
				lastErr = err
				return false
			}
			if len(modules) == 0 {
				lastErr = fmt.Errorf("artifact %s has no server-visible %s modules", a.Filename, r.key.ecosystem)
				continue
			}
			a.Modules = modules
			requirements := r.requirements
			if replacingArtifactless {
				requirements = append(slices.Clone(artifactless.Requirements), r.requirements...)
			}
			chosen[r.key] = types.LockedPackage{
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
			next := append([]projectRequest{}, rest...)
			for _, dep := range candidate.Dependencies {
				if dep.Kind == types.DependencyKindRequired || (withOptional && dep.Kind == types.DependencyKindOptional) {
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
					if dep.Kind == types.DependencyKindOptional {
						dependency.requirements = []types.RequirementBinding{
							{
								Reference: dep.Provider.String() + ":" + dep.ProjectID,
								Selector:  types.VersionStable,
								Enabled:   true,
							},
						}
					}
					next = append(next, dependency)
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
		}
		if lastErr == nil {
			lastErr = fmt.Errorf("no candidate satisfies %s:%s", r.key.provider, r.key.project)
		}
		return false
	}
	search := func(allowBroken bool) bool {
		chosen = map[projectKey]types.LockedPackage{}
		attempts = 0
		strict = !allowBroken
		lastErr = nil
		return backtrack(requests)
	}
	if !search(false) {
		if !acceptBroken {
			return nil, nil, lastErr
		}
		log.Debug(
			"resolve: retrying with broken graphs",
			"reason", lastErr,
		)
		if !search(true) {
			return nil, nil, lastErr
		}
	}
	var out []types.LockedPackage
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
			return nil, nil, errors.Join(problems...)
		}
		warnings = problems
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
func preferReleaseBuilds(list []types.CatalogueCandidate, requirements []types.RequirementBinding) []types.CatalogueCandidate {
	prefer := false
	for _, req := range requirements {
		if !req.Enabled {
			continue
		}
		if req.Selector == types.VersionAny || req.Selector == "" {
			prefer = true
		}
	}
	if !prefer {
		return list
	}
	ordered := make([]types.CatalogueCandidate, 0, len(list))
	for _, candidate := range list {
		if candidate.Channel == types.ReleaseChannelRelease {
			ordered = append(ordered, candidate)
		}
	}
	for _, candidate := range list {
		if candidate.Channel != types.ReleaseChannelRelease {
			ordered = append(ordered, candidate)
		}
	}
	return ordered
}

func packageEnabled(p types.LockedPackage) bool {
	if len(p.Requirements) == 0 {
		return true
	}
	for _, r := range p.Requirements {
		if r.Enabled {
			return true
		}
	}
	return false
}

func ValidateModules(packages []types.LockedPackage, server lockfile.Server, mcdrVersion string) ([]error, error) {
	type selected struct{ packageIndex, moduleIndex int }
	index := map[struct {
		environment types.Environment
		ecosystem   types.Ecosystem
		id          string
	}]selected{}
	var problems []error
	for pi := range packages {
		p := &packages[pi]
		if !packageEnabled(*p) {
			continue
		}
		for mi, m := range p.Modules {
			names := append([]string{m.ID}, m.Provides...)
			for _, name := range names {
				if m.Ecosystem == types.EcosystemBukkit || m.Ecosystem == types.EcosystemPaper {
					name = strings.ToLower(name)
				}
				key := struct {
					environment types.Environment
					ecosystem   types.Ecosystem
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
						scheme := types.VersionSchemeSemver
						if p.Ecosystem == types.EcosystemForge || p.Ecosystem == types.EcosystemNeoForge {
							scheme = types.VersionSchemeMaven
						}
						a, err := version.Parse(previous.Version, scheme)
						if err != nil {
							return nil, err
						}
						b, err := version.Parse(m.Version, scheme)
						if err != nil {
							return nil, err
						}
						comparison, isComparable := a.Compare(b)
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
		if !packageEnabled(*p) {
			continue
		}
		var selectedModules []types.DescriptorModule
		for mi := range p.Modules {
			m := p.Modules[mi]
			id := m.ID
			if m.Ecosystem == types.EcosystemBukkit || m.Ecosystem == types.EcosystemPaper {
				id = strings.ToLower(id)
			}
			key := struct {
				environment types.Environment
				ecosystem   types.Ecosystem
				id          string
			}{p.Environment, p.Ecosystem, id}
			if target := index[key]; target.packageIndex != pi || target.moduleIndex != mi {
				continue
			}
			for di := range m.Dependencies {
				dep := &m.Dependencies[di]
				if dep.Kind == types.DependencyKindPython {
					continue
				}
				value := ""
				builtin := true
				switch dep.ID {
				case "minecraft":
					value = string(server.Minecraft)
				case "fabricloader":
					if server.Product == types.ServerProductFabric {
						value = string(server.CoreVersion)
					}
				case "forge", "neoforge":
					if dep.ID == server.Product.String() {
						value = string(server.CoreVersion)
					}
				case "java":
					value = fmt.Sprintf("%d", server.Java)
				case "mcdreforged":
					value = mcdrVersion
				default:
					builtin = false
				}
				var target *types.LockedPackage
				var targetModule *types.DescriptorModule
				if !builtin {
					name := dep.ID
					if p.Ecosystem == types.EcosystemBukkit || p.Ecosystem == types.EcosystemPaper {
						name = strings.ToLower(name)
					}
					if match, ok := index[struct {
						environment types.Environment
						ecosystem   types.Ecosystem
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
					matched, err = version.Match(dep.Constraint, value, p.Ecosystem)
					if err != nil {
						problems = append(problems, fmt.Errorf("module %s requires %s %s: %w", m.ID, dep.ID, dep.Constraint, err))
						invalid = true
					}
				}
				requirement := strings.TrimSpace(dep.ID + " " + dep.Constraint)
				if !invalid && dep.Kind == types.DependencyKindRequired && !matched {
					if value == "" {
						problems = append(problems, fmt.Errorf("module %s requires %s, which no selected package provides", m.ID, requirement))
					} else {
						problems = append(problems, fmt.Errorf("module %s requires %s (selected %s)", m.ID, requirement, value))
					}
				}
				if !invalid && dep.Kind == types.DependencyKindIncompatible && matched {
					problems = append(problems, fmt.Errorf("module %s breaks %s %s", m.ID, dep.ID, value))
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

func mergeArtifactSelections(packages []types.LockedPackage) []types.LockedPackage {
	type key struct {
		environment            types.Environment
		ecosystem              types.Ecosystem
		provider               types.Provider
		project, release, file string
	}
	index := map[key]int{}
	out := make([]types.LockedPackage, 0, len(packages))
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
func sortPackages(packages []types.LockedPackage) {
	slices.SortFunc(packages, func(a, b types.LockedPackage) int {
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
