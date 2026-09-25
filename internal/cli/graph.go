package cli

import (
	"sort"

	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/workspace"
)

type GraphNode struct {
	// ID is the source-qualified stable package key (for example,
	// "modrinth:fabric-api").
	ID string

	Version string

	// Source is the origin of this package (modrinth, curseforge, github, mcdr, direct).
	Source string

	Optional bool

	Embedded bool

	Children []*GraphNode

	Parents []*GraphNode

	// InDegree is populated after graph build: the number of parents.
	InDegree int
}

type DependencyGraph struct {
	Nodes map[string]*GraphNode

	// Roots are the directly requested packages.
	Roots []*GraphNode
}

// BuildGraphFromLock builds the graph from a lock file's resolved packages.
// A package with at most one provenance entry is a root; deeper packages link
// to their Requester.
func BuildGraphFromLock(lock state.Lock) (*DependencyGraph, error) {
	graph := &DependencyGraph{
		Nodes: make(map[string]*GraphNode),
	}

	for _, p := range lock.Packages {
		node := &GraphNode{
			ID:       p.ID,
			Version:  p.Version,
			Source:   p.Source,
			Optional: p.Optional,
			Embedded: p.Embedded,
		}
		graph.Nodes[p.ID] = node
	}

	for _, p := range lock.Packages {
		node := graph.Nodes[p.ID]

		if len(p.Provenance) == 0 || len(p.Provenance) == 1 {
			graph.Roots = append(graph.Roots, node)
			continue
		}

		if p.Requester != "" {
			if parent, ok := graph.Nodes[p.Requester]; ok {
				parent.Children = append(parent.Children, node)
				node.Parents = append(node.Parents, parent)
			}
		}
	}

	for _, node := range graph.Nodes {
		node.InDegree = len(node.Parents)
	}

	return graph, nil
}

// BuildGraphFromProbe builds the graph from a probed server's packages.
// A package that no other package lists as a dependency is a root;
// parent-child relationships come from each package's Dependencies field.
func BuildGraphFromProbe(info workspace.Workspace) (*DependencyGraph, error) {
	graph := &DependencyGraph{
		Nodes: make(map[string]*GraphNode),
	}

	for _, p := range info.Packages {
		id := p.Id.StringBase()
		node := &GraphNode{
			ID:      id,
			Version: string(p.Id.Version),
		}
		graph.Nodes[id] = node
	}

	isDependency := make(map[string]bool)

	for _, p := range info.Packages {
		parentID := p.Id.StringBase()

		if len(p.Dependencies.Value) == 0 {
			continue
		}

		for _, dep := range p.Dependencies.Value {
			depID := dep.Id.StringBase()
			isDependency[depID] = true

			parent, parentOK := graph.Nodes[parentID]
			child, childOK := graph.Nodes[depID]

			if parentOK && childOK {
				parent.Children = append(parent.Children, child)
				child.Parents = append(child.Parents, parent)
			}
		}
	}

	for id, node := range graph.Nodes {
		if !isDependency[id] {
			graph.Roots = append(graph.Roots, node)
		}
	}

	for _, node := range graph.Nodes {
		node.InDegree = len(node.Parents)
	}

	return graph, nil
}

// GetLeaves returns non-root nodes with no dependents, sorted by ID.
func (g *DependencyGraph) GetLeaves() []*GraphNode {
	rootSet := make(map[string]bool, len(g.Roots))
	for _, r := range g.Roots {
		rootSet[r.ID] = true
	}

	var leaves []*GraphNode
	for _, node := range g.Nodes {
		if node.InDegree == 0 && !rootSet[node.ID] {
			leaves = append(leaves, node)
		}
	}

	sort.Slice(
		leaves, func(i, j int) bool {
			return leaves[i].ID < leaves[j].ID
		},
	)

	return leaves
}

func (g *DependencyGraph) GetRoots() []*GraphNode {
	return g.Roots
}

// TopologicalSort returns nodes in dependency-first order (dependencies
// before dependents). It returns nil if a cycle is detected.
func (g *DependencyGraph) TopologicalSort() []*GraphNode {
	outDeg := make(map[string]int, len(g.Nodes))
	for id := range g.Nodes {
		outDeg[id] = len(g.Nodes[id].Children)
	}

	queue := make([]string, 0, len(g.Nodes))
	for id, deg := range outDeg {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	result := make([]*GraphNode, 0, len(g.Nodes))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		result = append(result, g.Nodes[id])
		for _, parent := range g.Nodes[id].Parents {
			outDeg[parent.ID]--
			if outDeg[parent.ID] == 0 {
				queue = append(queue, parent.ID)
			}
		}
	}

	if len(result) != len(g.Nodes) {
		return nil
	}
	return result
}
