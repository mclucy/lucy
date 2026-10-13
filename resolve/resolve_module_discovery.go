package resolve

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/mclucy/lucy/artifact"
	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/upstream/routing"
)

// discoveryHits bounds how many catalogue projects one module identity inspects
// before resolution stops looking for the project that ships it.
const discoveryHits = 10

// moduleDiscovery maps loader module identities onto the catalogue projects
// shipping them. Catalogue metadata names dependencies by project, while jar
// metadata names them by module identity, and no upstream publishes the
// translation between the two. Resolution needs it because a required
// dependency the registry omitted reaches the graph only from the artifact.
type moduleDiscovery struct {
	server   lockfile.Server
	acquire  func(*types.Artifact) (string, error)
	requests map[string]projectRequest
	absent   map[string]bool
	failures map[string]error
}

func newModuleDiscovery(server lockfile.Server, acquire func(*types.Artifact) (string, error)) *moduleDiscovery {
	return &moduleDiscovery{
		server:   server,
		acquire:  acquire,
		requests: map[string]projectRequest{},
		absent:   map[string]bool{},
		failures: map[string]error{},
	}
}

// project returns the catalogue project carrying a module identity in one
// runtime. A report that no project ships the identity is not an error: the
// graph may already carry it through a package selected elsewhere, so module
// validation settles that case. Only a failed lookup errors.
func (index *moduleDiscovery) project(ctx context.Context, from projectKey, id string) (projectRequest, bool, error) {
	if request, ok := index.requests[id]; ok {
		log.Debug(
			"resolve: module discovery reused",
			"module", id,
			"provider", request.key.provider,
			"project", request.key.project,
			"reason", "module identity already mapped",
		)
		return request, true, nil
	}
	if index.absent[id] {
		log.Debug(
			"resolve: module discovery reused absence",
			"module", id,
			"reason", "no catalogue project ships module",
		)
		return projectRequest{}, false, nil
	}
	if err, ok := index.failures[id]; ok {
		log.Debug(
			"resolve: module discovery reused failure",
			"module", id,
			"reason", "lookup failed earlier in resolution",
		)
		return projectRequest{}, false, err
	}
	request, found, err := index.search(ctx, from, id)
	if err != nil {
		index.failures[id] = err
		return projectRequest{}, false, err
	}
	if !found {
		index.absent[id] = true
		return projectRequest{}, false, nil
	}
	index.requests[id] = request
	return request, true, nil
}

func (index *moduleDiscovery) search(ctx context.Context, from projectKey, id string) (projectRequest, bool, error) {
	searchers, err := routing.ResolveSearchProviders(from.loader, types.SourceAuto)
	if err != nil {
		return projectRequest{}, false, err
	}
	log.Debug(
		"resolve: module discovery searching",
		"module", id,
		"loader", from.loader,
		"reason", "module identity missing from catalogue dependency",
	)
	responses, _ := routing.SearchMany(searchers, id, upstream.SearchOptions{FilterEcosystem: from.loader})
	inspected := 0
	for _, hit := range collectDiscoveryHits(responses) {
		if inspected == discoveryHits {
			break
		}
		inspected++
		request, found, err := index.verify(ctx, from, hit, id)
		if err != nil {
			return projectRequest{}, false, err
		}
		if found {
			return request, true, nil
		}
	}
	return projectRequest{}, false, nil
}

// verify reports whether the newest release of one catalogue project carries
// the module identity, so a search hit becomes a dependency only on proof.
func (index *moduleDiscovery) verify(ctx context.Context, from projectKey, hit catalogueProject, id string) (projectRequest, bool, error) {
	log.Debug(
		"resolve: module discovery verifying candidate",
		"module", id,
		"provider", hit.provider,
		"project", hit.project,
	)
	list, err := routing.Candidates(ctx, hit.provider, hit.project, index.server.Minecraft, from.loader)
	if err != nil {
		if errors.Is(err, routing.ErrNoCatalogue) {
			log.Debug(
				"resolve: module discovery candidate rejected",
				"module", id,
				"provider", hit.provider,
				"project", hit.project,
				"reason", "source has no package catalogue",
			)
			return projectRequest{}, false, nil
		}
		return projectRequest{}, false, fmt.Errorf("resolve module %s: %w", id, err)
	}
	if len(list) == 0 {
		log.Debug(
			"resolve: module discovery candidate rejected",
			"module", id,
			"provider", hit.provider,
			"project", hit.project,
			"reason", "project has no candidates",
		)
		return projectRequest{}, false, nil
	}
	slices.SortStableFunc(list, func(a, b types.CatalogueCandidate) int {
		return strings.Compare(b.Published, a.Published)
	})
	candidate := list[0].Artifact
	path, err := index.acquire(&candidate)
	if err != nil {
		return projectRequest{}, false, fmt.Errorf("resolve module %s: %w", id, err)
	}
	modules, err := artifact.Inspect(ctx, path, from.loader)
	if err != nil {
		return projectRequest{}, false, fmt.Errorf("resolve module %s: %w", id, err)
	}
	project := projectRequest{key: projectKey{from.runtime, from.loader, hit.provider, hit.project}}
	for _, module := range modules {
		if moduleAlias(module, id) {
			log.Debug(
				"resolve: module discovery verified",
				"module", id,
				"provider", hit.provider,
				"project", hit.project,
				"reason", "artifact declares module identity",
			)
			return project, true, nil
		}
	}
	log.Debug(
		"resolve: module discovery candidate rejected",
		"module", id,
		"provider", hit.provider,
		"project", hit.project,
		"reason", "artifact does not declare module identity",
	)
	return projectRequest{}, false, nil
}

type catalogueProject struct {
	provider, project string
}

func collectDiscoveryHits(responses []upstream.SearchResponse) []catalogueProject {
	hits := []catalogueProject{}
	seen := map[catalogueProject]bool{}
	for _, response := range responses {
		for _, item := range response.Items {
			hit := catalogueProject{response.Source.String(), item.RemoteName}
			if seen[hit] {
				continue
			}
			seen[hit] = true
			hits = append(hits, hit)
		}
	}
	return hits
}

// moduleAlias reports whether a native module publishes a loader module
// identity as its own identifier or as an alias.
func moduleAlias(module types.NativeModule, id string) bool {
	if module.Loader == types.EcoBukkit {
		return strings.EqualFold(module.ID, id) ||
			slices.ContainsFunc(module.Provides, func(alias string) bool { return strings.EqualFold(alias, id) })
	}
	return module.ID == id || slices.Contains(module.Provides, id)
}

// providesSelected reports whether a package already selected in the current
// search state carries the module identity, which makes discovery unnecessary.
func providesSelected(chosen map[projectKey]types.LockedPackage, loader types.Ecosystem, id string) bool {
	for _, selected := range chosen {
		if selected.Loader != loader {
			continue
		}
		for _, module := range selected.Modules {
			if moduleAlias(module, id) {
				return true
			}
		}
	}
	return false
}

// runtimeModule reports whether a module identity is satisfied by the server
// runtime itself, so resolution never searches for a project shipping it.
func runtimeModule(id string) bool {
	switch id {
	case "minecraft", "fabricloader", "forge", "neoforge", "java", "mcdreforged":
		return true
	default:
		return false
	}
}
