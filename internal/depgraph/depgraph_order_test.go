package depgraph

import (
	"slices"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/server"
)

func TestOrderedArtifacts(t *testing.T) {
	const (
		environment = server.EnvironmentServer
		ecosystem   = catalog.EcosystemFabric
	)
	artifact := func(id string) *Node {
		return &Node{
			ID:          id,
			Environment: environment,
			Ecosystem:   ecosystem,
		}
	}
	module := func(owner *Node, id string) *Node {
		return &Node{
			ID:          id,
			Artifact:    owner.ID,
			Environment: environment,
			Ecosystem:   ecosystem,
			Module:      true,
		}
	}
	link := func(parent, child *Node) {
		parent.Children = append(parent.Children, child)
		child.Parents = append(child.Parents, parent)
	}
	buildGraph := func(nodes ...*Node) *Graph {
		built := &Graph{Nodes: make(map[string]*Node, len(nodes))}
		for _, node := range nodes {
			built.Nodes[node.Key()] = node
		}
		return built
	}
	ids := func(nodes []*Node) []string {
		ids := make([]string, 0, len(nodes))
		for _, node := range nodes {
			ids = append(ids, node.ID)
		}
		return ids
	}
	// The graph is map-backed, so callers rely on the same order on every call
	// and on each artifact appearing exactly once.
	stableArtifacts := func(t *testing.T, graph *Graph) []string {
		t.Helper()
		want := ids(graph.OrderedArtifacts())
		seen := make(map[string]bool, len(want))
		for _, id := range want {
			if seen[id] {
				t.Fatalf("OrderedArtifacts() repeated %q: %v", id, want)
			}
			seen[id] = true
		}
		for range 50 {
			if got := ids(graph.OrderedArtifacts()); !slices.Equal(got, want) {
				t.Fatalf("OrderedArtifacts() = %v, first call = %v", got, want)
			}
		}
		return want
	}
	requireBefore := func(t *testing.T, order []string, dependency, dependent string) {
		t.Helper()
		dependencyIndex := slices.Index(order, dependency)
		dependentIndex := slices.Index(order, dependent)
		if dependencyIndex < 0 || dependentIndex < 0 {
			t.Fatalf("artifacts %q and %q missing from order %v", dependency, dependent, order)
		}
		if dependencyIndex > dependentIndex {
			t.Fatalf("dependency %q follows dependent %q in %v", dependency, dependent, order)
		}
	}

	t.Run("module provider precedes its dependent", func(t *testing.T) {
		provider := artifact("modrinth:provider")
		providerAPI := module(provider, "provider-api")
		providerDeep := module(provider, "provider-deep")
		dependent := artifact("modrinth:dependent")
		dependentModule := module(dependent, "dependent")

		// The provider ships a second module, so the provider artifact is not
		// ready until that module's own dependencies resolve. The dependent
		// only needs the API module and must still follow the provider.
		link(provider, providerAPI)
		link(provider, providerDeep)
		link(providerDeep, providerAPI)
		link(dependent, dependentModule)
		link(dependentModule, providerAPI)

		order := stableArtifacts(t, buildGraph(
			provider,
			providerAPI,
			providerDeep,
			dependent,
			dependentModule,
		))
		requireBefore(t, order, provider.ID, dependent.ID)
	})

	t.Run("direct artifact edge is preserved", func(t *testing.T) {
		provider := artifact("modrinth:provider")
		dependent := artifact("modrinth:dependent")
		link(dependent, provider)

		order := stableArtifacts(t, buildGraph(provider, dependent))
		requireBefore(t, order, provider.ID, dependent.ID)
	})

	t.Run("cycle still lists every artifact once", func(t *testing.T) {
		provider := artifact("modrinth:provider")
		dependent := artifact("modrinth:dependent")
		link(dependent, provider)
		link(provider, dependent)

		stableArtifacts(t, buildGraph(provider, dependent))
	})
}
