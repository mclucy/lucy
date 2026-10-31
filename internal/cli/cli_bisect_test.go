package cli

import (
	"slices"
	"testing"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/server"
)

func TestBisectOrderedArtifacts(t *testing.T) {
	const (
		environment = server.EnvironmentServer
		ecosystem   = catalog.EcosystemFabric
	)
	artifact := func(id string) *GraphNode {
		return &GraphNode{
			ID:          id,
			Environment: environment,
			Ecosystem:   ecosystem,
		}
	}
	module := func(owner *GraphNode, id string) *GraphNode {
		return &GraphNode{
			ID:          id,
			Artifact:    owner.ID,
			Environment: environment,
			Ecosystem:   ecosystem,
			Module:      true,
		}
	}
	link := func(parent, child *GraphNode) {
		parent.Children = append(parent.Children, child)
		child.Parents = append(child.Parents, parent)
	}
	buildGraph := func(nodes ...*GraphNode) *DependencyGraph {
		built := &DependencyGraph{Nodes: make(map[string]*GraphNode, len(nodes))}
		for _, node := range nodes {
			built.Nodes[node.key()] = node
		}
		return built
	}
	ids := func(nodes []*GraphNode) []string {
		ids := make([]string, 0, len(nodes))
		for _, node := range nodes {
			ids = append(ids, node.ID)
		}
		return ids
	}
	requireOrder := func(t *testing.T, graph *DependencyGraph, want ...string) {
		t.Helper()
		for range 50 {
			got := ids(bisectOrderedArtifacts(graph))
			if !slices.Equal(got, want) {
				t.Fatalf("bisectOrderedArtifacts() = %v, want %v", got, want)
			}
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

		requireOrder(
			t,
			buildGraph(
				provider,
				providerAPI,
				providerDeep,
				dependent,
				dependentModule,
			),
			provider.ID,
			dependent.ID,
		)
	})

	t.Run("direct artifact edge is preserved", func(t *testing.T) {
		provider := artifact("modrinth:provider")
		dependent := artifact("modrinth:dependent")
		link(dependent, provider)

		requireOrder(t, buildGraph(provider, dependent), provider.ID, dependent.ID)
	})

	t.Run("cycle falls back to identifier order", func(t *testing.T) {
		provider := artifact("modrinth:provider")
		dependent := artifact("modrinth:dependent")
		link(dependent, provider)
		link(provider, dependent)

		requireOrder(t, buildGraph(provider, dependent), dependent.ID, provider.ID)
	})
}
