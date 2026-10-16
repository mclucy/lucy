package resolve

import (
	"context"
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
		runtime           string
		loader            types.Ecosystem
		provider, project string
	}
	projectRequest struct {
		key          projectKey
		requirements []types.RequirementBinding
		releaseIDs   []string
	}
)

func Packages(ctx context.Context, d *manifest.Document, previous *lockfile.Document, server lockfile.Server, mcdrVersion string, acquire func(*types.Artifact) (string, error), withOptional bool) ([]types.LockedPackage, error) {
	requests := []projectRequest{}
	appendRequest := func(runtime string, loader types.Ecosystem, ref string, r manifest.Requirement) error {
		provider, project, err := manifest.ParseReference(ref)
		if err != nil {
			return err
		}
		if previous != nil {
			for _, p := range previous.Packages {
				if p.Runtime == runtime && p.Loader == loader && p.Provider == provider {
					for _, old := range p.Requirements {
						if old.Reference == ref {
							project = p.ProjectID
						}
					}
				}
			}
		}
		requests = append(requests, projectRequest{key: projectKey{runtime, loader, provider, project}, requirements: []types.RequirementBinding{{Reference: ref, Selector: r.Selector(), File: r.File, Enabled: r.IsEnabled()}}})
		return nil
	}
	loaders := []types.Ecosystem{}
	for loader := range d.Server.Packages {
		loaders = append(loaders, loader)
	}
	slices.Sort(loaders)
	for _, loader := range loaders {
		refs := []string{}
		for ref := range d.Server.Packages[loader] {
			refs = append(refs, ref)
		}
		slices.Sort(refs)
		for _, ref := range refs {
			if err := appendRequest("server", loader, ref, d.Server.Packages[loader][ref]); err != nil {
				return nil, err
			}
		}
	}
	if d.MCDR != nil {
		refs := []string{}
		for ref := range d.MCDR.Packages {
			refs = append(refs, ref)
		}
		slices.Sort(refs)
		for _, ref := range refs {
			if err := appendRequest("mcdr", types.EcoMcdr, ref, d.MCDR.Packages[ref]); err != nil {
				return nil, err
			}
		}
	}
	for _, r := range requests {
		if r.key.runtime == "server" {
			supported := false
			switch server.Distribution {
			case "fabric":
				supported = r.key.loader == types.EcoFabric
			case "forge":
				supported = r.key.loader == types.EcoForge
			case "neoforge":
				supported = r.key.loader == types.EcoNeoforge
			case "paper", "purpur":
				supported = r.key.loader == types.EcoBukkit
			default:
				supported = server.Bootstrap == "paperclip" && r.key.loader == types.EcoBukkit
			}
			if !supported {
				for _, req := range r.requirements {
					if req.Enabled {
						return nil, fmt.Errorf("distribution %s cannot load %s packages", server.Distribution, r.key.loader)
					}
				}
			}
		}
	}
	candidates := map[projectKey][]types.CatalogueCandidate{}
	chosen := map[projectKey]types.LockedPackage{}
	discovery := newModuleDiscovery(server, acquire)
	attempts := 0
	var lastErr error
	log.Info(
		"resolve: resolving package requirements",
		"requests", len(requests),
	)
	var search func([]projectRequest) bool
	search = func(pending []projectRequest) bool {
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
			if err := ValidateModules(all, server, mcdrVersion); err != nil {
				lastErr = err
				return false
			}
			return true
		}
		r := pending[0]
		rest := pending[1:]
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
			if search(rest) {
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
			p := types.LockedPackage{Runtime: r.key.runtime, Loader: r.key.loader, Provider: r.key.provider, ProjectID: r.key.project, Requirements: r.requirements}
			chosen[r.key] = p
			if search(rest) {
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
			list, err = routing.Candidates(ctx, r.key.provider, r.key.project, server.Minecraft, r.key.loader)
			if err != nil {
				lastErr = err
				return false
			}
			slices.SortStableFunc(list, func(a, b types.CatalogueCandidate) int { return strings.Compare(b.Published, a.Published) })
			if previous != nil {
				for _, p := range previous.Packages {
					if p.Runtime == r.key.runtime && p.Loader == r.key.loader && p.Provider == r.key.provider && p.ProjectID == r.key.project {
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
				case "stable":
					if candidate.ReleaseType != "release" {
						reject("not a release build")
					}
				case "any", "beta":
				default:
					if candidate.Version != req.Selector && candidate.ReleaseID != req.Selector && candidate.FileID != req.Selector && candidate.Filename != req.Selector {
						reject("selector mismatch")
					}
				}
				if req.File != "" {
					match, err := filepath.Match(req.File, candidate.Filename)
					if err != nil {
						lastErr = err
						return false
					}
					if !match {
						reject("file pattern mismatch")
					}
				} else if !candidate.Primary {
					reject("not the primary file")
				}
			}
			// A dependency request carries no requirement of its own, so no
			// enabled binding rejects the extra files a release publishes: the
			// sources and dev jars carry a readable descriptor and would
			// otherwise win the version race on publication date alone.
			if !constrained && !candidate.Primary {
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
			p, err := acquire(&a)
			if err != nil {
				lastErr = err
				return false
			}
			modules, err := artifact.Inspect(ctx, p, r.key.loader)
			if err != nil {
				lastErr = err
				return false
			}
			if len(modules) == 0 {
				lastErr = fmt.Errorf("artifact %s has no server-visible %s modules", a.Filename, r.key.loader)
				continue
			}
			a.Modules = modules
			requirements := r.requirements
			if replacingArtifactless {
				requirements = append(slices.Clone(artifactless.Requirements), r.requirements...)
			}
			chosen[r.key] = types.LockedPackage{Runtime: r.key.runtime, Loader: r.key.loader, Artifact: a, Requirements: requirements}
			log.Debug(
				"resolve: candidate selected",
				"provider", r.key.provider,
				"project", r.key.project,
				"candidate", candidate.Version,
				"file", candidate.Filename,
				"modules", len(modules),
			)
			next := append([]projectRequest{}, rest...)
			declared := map[catalogueProject]bool{}
			for _, dep := range candidate.Dependencies {
				if dep.Kind == "required" || (withOptional && dep.Kind == "optional") {
					declared[catalogueProject{dep.Provider, dep.ProjectID}] = true
					ids := []string{}
					if dep.ReleaseID != "" {
						ids = append(ids, dep.ReleaseID)
					}
					dependency := projectRequest{key: projectKey{r.key.runtime, r.key.loader, dep.Provider, dep.ProjectID}, releaseIDs: ids}
					if dep.Kind == "optional" {
						dependency.requirements = []types.RequirementBinding{{Reference: dep.Provider + ":" + dep.ProjectID, Selector: "stable", Enabled: true}}
					}
					next = append(next, dependency)
				}
			}
			// A required dependency the catalogue omitted still has to enter
			// the graph, so resolution asks the registry which project ships
			// the module identity the artifact itself declares.
			for _, module := range a.Modules {
				for _, dep := range module.Dependencies {
					if dep.Kind != "required" {
						continue
					}
					if runtimeModule(dep.ID) {
						log.Debug(
							"resolve: module dependency skipped",
							"provider", r.key.provider,
							"project", r.key.project,
							"module", dep.ID,
							"reason", "provided by server runtime",
						)
						continue
					}
					if providesSelected(chosen, r.key.loader, dep.ID) {
						log.Debug(
							"resolve: module dependency skipped",
							"provider", r.key.provider,
							"project", r.key.project,
							"module", dep.ID,
							"reason", "provided by selected package",
						)
						continue
					}
					request, found, err := discovery.project(ctx, r.key, dep.ID)
					if err != nil {
						lastErr = err
						return false
					}
					if !found {
						log.Debug(
							"resolve: module dependency unresolved",
							"provider", r.key.provider,
							"project", r.key.project,
							"module", dep.ID,
							"reason", "no catalogue project ships module",
						)
						continue
					}
					if declared[catalogueProject{request.key.provider, request.key.project}] {
						log.Debug(
							"resolve: module dependency skipped",
							"provider", r.key.provider,
							"project", r.key.project,
							"module", dep.ID,
							"reason", "declared in catalogue metadata",
						)
						continue
					}
					next = append(next, request)
					log.Debug(
						"resolve: native dependency discovered",
						"module", dep.ID,
						"provider", request.key.provider,
						"project", request.key.project,
						"reason", "artifact declares module identity",
					)
				}
			}
			if search(next) {
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
	if !search(requests) {
		return nil, lastErr
	}
	out := []types.LockedPackage{}
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
	slices.SortFunc(out, func(a, b types.LockedPackage) int {
		if n := strings.Compare(a.Runtime, b.Runtime); n != 0 {
			return n
		}
		if n := strings.Compare(string(a.Loader), string(b.Loader)); n != 0 {
			return n
		}
		if n := strings.Compare(a.Provider, b.Provider); n != 0 {
			return n
		}
		return strings.Compare(a.ProjectID, b.ProjectID)
	})
	if err := ValidateModules(out, server, mcdrVersion); err != nil {
		return nil, err
	}
	log.Info(
		"resolve: selected packages",
		"packages", len(out),
	)
	return out, nil
}

func packageActive(p types.LockedPackage) bool {
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

func ValidateModules(packages []types.LockedPackage, server lockfile.Server, mcdrVersion string) error {
	type selected struct{ packageIndex, moduleIndex int }
	index := map[struct {
		runtime string
		loader  types.Ecosystem
		id      string
	}]selected{}
	for pi := range packages {
		p := &packages[pi]
		if !packageActive(*p) {
			continue
		}
		for mi, m := range p.Modules {
			names := append([]string{m.ID}, m.Provides...)
			for _, name := range names {
				if m.Loader == types.EcoBukkit {
					name = strings.ToLower(name)
				}
				key := struct {
					runtime string
					loader  types.Ecosystem
					id      string
				}{p.Runtime, p.Loader, name}
				if old, ok := index[key]; ok {
					previous := packages[old.packageIndex].Modules[old.moduleIndex]
					if m.Member == "" && previous.Member == "" && pi != old.packageIndex {
						return fmt.Errorf("duplicate native module %s in %s and %s", name, p.Filename, packages[old.packageIndex].Filename)
					}
					if previous.Member == "" {
						continue
					}
					if m.Member != "" {
						scheme := types.Semver
						if p.Loader == types.EcoForge || p.Loader == types.EcoNeoforge {
							scheme = types.Maven
						}
						a, err := version.Parse(types.BareVersion(previous.Version), scheme)
						if err != nil {
							return err
						}
						b, err := version.Parse(types.BareVersion(m.Version), scheme)
						if err != nil {
							return err
						}
						comparison, comparable := a.Compare(b)
						if !comparable {
							return fmt.Errorf("incomparable nested module versions %s and %s", previous.Version, m.Version)
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
		if !packageActive(*p) {
			continue
		}
		selectedModules := []types.NativeModule{}
		for mi := range p.Modules {
			m := p.Modules[mi]
			id := m.ID
			if m.Loader == types.EcoBukkit {
				id = strings.ToLower(id)
			}
			key := struct {
				runtime string
				loader  types.Ecosystem
				id      string
			}{p.Runtime, p.Loader, id}
			if target := index[key]; target.packageIndex != pi || target.moduleIndex != mi {
				continue
			}
			for di := range m.Dependencies {
				dep := &m.Dependencies[di]
				if dep.Kind == "python" {
					continue
				}
				value := ""
				builtin := true
				switch dep.ID {
				case "minecraft":
					value = server.Minecraft
				case "fabricloader":
					if server.Distribution == "fabric" {
						value = server.CoreVersion
					}
				case "forge", "neoforge":
					if dep.ID == server.Distribution {
						value = server.CoreVersion
					}
				case "java":
					value = fmt.Sprintf("%d", server.Java)
				case "mcdreforged":
					value = mcdrVersion
				default:
					builtin = false
				}
				var target *types.LockedPackage
				var targetModule *types.NativeModule
				if !builtin {
					name := dep.ID
					if p.Loader == types.EcoBukkit {
						name = strings.ToLower(name)
					}
					if match, ok := index[struct {
						runtime string
						loader  types.Ecosystem
						id      string
					}{p.Runtime, p.Loader, name}]; ok {
						target = &packages[match.packageIndex]
						targetModule = &target.Modules[match.moduleIndex]
						value = targetModule.Version
					}
				}
				matched := value != ""
				if matched && dep.Constraint != "" {
					var err error
					matched, err = version.Match(dep.Constraint, value, p.Loader)
					if err != nil {
						return fmt.Errorf("%s dependency %s: %w", m.ID, dep.ID, err)
					}
				}
				if dep.Kind == "required" && !matched {
					return fmt.Errorf("module %s requires %s %s (selected %s)", m.ID, dep.ID, dep.Constraint, value)
				}
				if dep.Kind == "incompatible" && matched {
					return fmt.Errorf("module %s breaks %s %s", m.ID, dep.ID, value)
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
	return nil
}

func mergeArtifactSelections(packages []types.LockedPackage) []types.LockedPackage {
	type key struct {
		runtime                          string
		loader                           types.Ecosystem
		provider, project, release, file string
	}
	index := map[key]int{}
	out := make([]types.LockedPackage, 0, len(packages))
	for _, p := range packages {
		k := key{p.Runtime, p.Loader, p.Provider, p.ProjectID, p.ReleaseID, p.FileID}
		if i, ok := index[k]; ok {
			out[i].Requirements = append(out[i].Requirements, p.Requirements...)
			continue
		}
		index[k] = len(out)
		out = append(out, p)
	}
	return out
}
