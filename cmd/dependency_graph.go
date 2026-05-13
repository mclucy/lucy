package cmd

import (
	"sort"

	"github.com/mclucy/lucy/state"
	"github.com/mclucy/lucy/types"
)

// GraphNode represents a single package in the dependency graph.
type GraphNode struct {
	// ID is the package identifier in "platform/name" format (e.g. "fabric/fabric-api").
	ID string

	// Version is the exact version string of this package.
	Version string

	// Source is the origin of this package (modrinth, curseforge, github, mcdr, direct).
	Source string

	// Optional indicates whether this package is marked as optional.
	Optional bool

	// Embedded indicates whether this package is embedded in its parent.
	Embedded bool

	// Children are the packages this node depends on.
	Children []*GraphNode

	// Parents are the packages that depend on this node.
	Parents []*GraphNode

	// InDegree is the number of parents (dependents) calculated after graph build.
	InDegree int
}

// DependencyGraph represents the full dependency graph of packages.
type DependencyGraph struct {
	// Nodes maps package IDs to their graph nodes.
	Nodes map[string]*GraphNode

	// Roots are the root nodes (directly requested packages).
	Roots []*GraphNode
}

// BuildGraphFromLock builds a dependency graph from a lock file's resolved packages.
// Root nodes are identified as packages with zero or one provenance entries.
// Parent-child relationships are established via the Requester field.
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

// BuildGraphFromProbe builds a dependency graph from a probed server's packages.
// Root nodes are identified as packages that are not listed as a dependency of
// any other package. Parent-child relationships are established via each package's
// Dependencies field.
func BuildGraphFromProbe(info types.ServerInfo) (*DependencyGraph, error) {
	graph := &DependencyGraph{
		Nodes: make(map[string]*GraphNode),
	}

	for _, p := range info.Packages {
		id := string(p.Id.Platform) + "/" + string(p.Id.Name)
		node := &GraphNode{
			ID:      id,
			Version: string(p.Id.Version),
		}
		graph.Nodes[id] = node
	}

	isDependency := make(map[string]bool)

	for _, p := range info.Packages {
		parentID := string(p.Id.Platform) + "/" + string(p.Id.Name)

		if p.Dependencies == nil {
			continue
		}

		for _, dep := range p.Dependencies.Value {
			depID := string(dep.Id.Platform) + "/" + string(dep.Id.Name)
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

// GetLeaves returns nodes with InDegree == 0 that are NOT root nodes.
// These are packages that nothing depends on, excluding explicitly installed ones.
// Results are sorted alphabetically by ID.
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

	sort.Slice(leaves, func(i, j int) bool {
		return leaves[i].ID < leaves[j].ID
	})

	return leaves
}

// GetRoots returns the root nodes (directly requested packages).
func (g *DependencyGraph) GetRoots() []*GraphNode {
	return g.Roots
}
