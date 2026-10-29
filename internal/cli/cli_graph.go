package cli

import (
	"cmp"
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/workspace"
)

// GraphNode is one node of a resolved package graph. A node is either an
// artifact coordinate ("provider:project", taken from the lock's explicit
// provider fields) or a descriptor module (a module ID declared inside an
// artifact). Identity is always an explicit field; nothing is recovered by
// parsing a display string.
type GraphNode struct {
	// ID is the native module ID, or "provider:project" for an artifact.
	ID string

	Version string

	// Provider and ProjectID carry the upstream coordinates of an artifact
	// node. They are empty for a native module node, which belongs to the
	// artifact recorded in Artifact.
	Provider  catalog.Provider
	ProjectID string

	// Artifact is the coordinate node that ships this module. It is empty
	// for artifact nodes themselves.
	Artifact string

	Filename string

	// Environment scopes the node ("server" or "mcdr") and Ecosystem scopes
	// it within that environment. Dependency edges only resolve inside one
	// scope.
	Environment server.Environment
	Ecosystem   catalog.Ecosystem

	// Module reports whether the node is a native module rather than an
	// artifact coordinate.
	Module bool

	// Root reports a node that an enabled manifest requirement selects.
	Root bool

	Children []*GraphNode

	Parents []*GraphNode
}

type DependencyGraph struct {
	// Nodes are keyed by the scoped node key, so identically named modules
	// in different environments or ecosystems stay distinct.
	Nodes map[string]*GraphNode

	// Roots are the artifacts an enabled manifest requirement selects.
	Roots []*GraphNode
}

// nodeKey scopes an identity so edges never cross environments or
// ecosystems.
func nodeKey(
	environment server.Environment,
	ecosystem catalog.Ecosystem,
	id string,
) string {
	return string(environment) + "|" + string(ecosystem) + "|" + id
}

// artifactCoordinate renders an artifact's explicit provider coordinates.
func artifactCoordinate(provider catalog.Provider, projectID string) string {
	return provider.String() + ":" + projectID
}

// BuildGraphFromLock builds the graph from a lock's resolved packages.
//
// Roots are the artifacts selected by an enabled manifest requirement.
// Module dependency edges come from each artifact's native module
// descriptors. The server core and bootstrap inputs are lock inputs, not
// packages, so they never become graph nodes.
func BuildGraphFromLock(lock *document.Lock) (*DependencyGraph, error) {
	graph := &DependencyGraph{
		Nodes: make(map[string]*GraphNode),
	}

	dependencies := make(map[string][]catalog.DescriptorDependency)

	for _, pkg := range lock.Packages {
		coordinate := artifactCoordinate(pkg.Provider, pkg.ProjectID)
		key := nodeKey(pkg.Environment, pkg.Ecosystem, coordinate)

		node, ok := graph.Nodes[key]
		if !ok {
			node = &GraphNode{
				ID:          coordinate,
				Version:     pkg.Version.String(),
				Provider:    pkg.Provider,
				ProjectID:   pkg.ProjectID,
				Filename:    pkg.Filename,
				Environment: pkg.Environment,
				Ecosystem:   pkg.Ecosystem,
			}
			graph.Nodes[key] = node
		}

		for _, module := range pkg.Modules {
			moduleKey := nodeKey(pkg.Environment, pkg.Ecosystem, module.ID)
			if _, exists := graph.Nodes[moduleKey]; !exists {
				graph.Nodes[moduleKey] = &GraphNode{
					ID:          module.ID,
					Version:     module.Version.String(),
					Artifact:    coordinate,
					Environment: pkg.Environment,
					Ecosystem:   pkg.Ecosystem,
					Module:      true,
					Filename:    pkg.Filename,
				}
			}
			dependencies[moduleKey] = module.Dependencies
		}

		for _, binding := range pkg.Requirements {
			if !binding.Enabled {
				continue
			}
			node.Root = true
			graph.Roots = append(graph.Roots, node)
			break
		}
	}

	// A root artifact hangs its own top-level modules so the tree has
	// something to show beneath the requirement.
	for _, root := range graph.Roots {
		for _, node := range graph.Nodes {
			if node.Module && node.Artifact == root.ID &&
				node.Environment == root.Environment &&
				node.Ecosystem == root.Ecosystem {
				root.Children = append(root.Children, node)
				node.Parents = append(node.Parents, root)
			}
		}
	}

	// Dependency edges stay inside one environment/ecosystem scope.
	for _, node := range graph.Nodes {
		if !node.Module {
			continue
		}
		for _, dep := range dependencies[node.key()] {
			child := resolveDependencyNode(
				graph,
				node.Environment,
				node.Ecosystem,
				dep,
			)
			if child == nil || child == node {
				continue
			}
			if !slices.Contains(child.Parents, node) {
				node.Children = append(node.Children, child)
				child.Parents = append(child.Parents, node)
			}
		}
	}

	slices.SortFunc(graph.Roots, func(a, b *GraphNode) int {
		if c := cmp.Compare(a.ID, b.ID); c != 0 {
			return c
		}
		return cmp.Compare(a.key(), b.key())
	})
	return graph, nil
}

func (n *GraphNode) key() string {
	return nodeKey(n.Environment, n.Ecosystem, n.ID)
}

// resolveDependencyNode maps a declared dependency onto a graph node. A
// native dependency names a module ID; when it instead carries upstream
// project coordinates, it resolves to the artifact that ships them.
func resolveDependencyNode(
	graph *DependencyGraph,
	environment server.Environment,
	ecosystem catalog.Ecosystem,
	dep catalog.DescriptorDependency,
) *GraphNode {
	if dep.ModuleID != "" {
		if node, ok := graph.Nodes[nodeKey(environment, ecosystem, dep.ModuleID)]; ok {
			return node
		}
	}
	if dep.Provider != catalog.ProviderUnspecified && dep.ProjectID != "" {
		coordinate := artifactCoordinate(dep.Provider, dep.ProjectID)
		if node, ok := graph.Nodes[nodeKey(environment, ecosystem, coordinate)]; ok {
			return node
		}
	}
	if dep.ModuleID == "" && dep.ID != "" {
		if node, ok := graph.Nodes[nodeKey(environment, ecosystem, dep.ID)]; ok {
			return node
		}
	}
	return nil
}

// BuildGraphFromObservation builds the graph from an observed server's
// packages. A package that no other package lists as a dependency is a root;
// parent-child relationships come from each package's Dependencies field.
func BuildGraphFromObservation(info *workspace.Workspace) (*DependencyGraph, error) {
	graph := &DependencyGraph{
		Nodes: make(map[string]*GraphNode),
	}

	for _, p := range info.Packages {
		id := p.ID.StringBase()
		node := &GraphNode{
			ID:          id,
			Version:     p.ID.Version.String(),
			Provider:    p.ID.Provider,
			ProjectID:   p.ID.Name.String(),
			Filename:    filepath.Base(p.Path),
			Environment: server.EnvironmentServer,
			Ecosystem:   p.ID.Ecosystem,
		}
		graph.Nodes[node.key()] = node
	}

	// A probe observation has no runtime membership marker, so every node
	// scopes to the server environment.
	isDependency := make(map[string]bool)

	for _, p := range info.Packages {
		parentID := p.ID.StringBase()
		parent, parentOK := graph.Nodes[nodeKey(
			server.EnvironmentServer,
			p.ID.Ecosystem,
			parentID,
		)]
		if !parentOK {
			continue
		}

		for _, dep := range p.Dependencies {
			depID := dep.ID.StringBase()

			depEcosystem := dep.ID.Ecosystem
			if depEcosystem == catalog.EcosystemUnspecified {
				depEcosystem = p.ID.Ecosystem
			}
			child, childOK := graph.Nodes[nodeKey(
				server.EnvironmentServer,
				depEcosystem,
				depID,
			)]
			if !childOK || child == parent {
				continue
			}
			isDependency[child.key()] = true

			if slices.Contains(parent.Children, child) {
				continue
			}
			parent.Children = append(parent.Children, child)
			child.Parents = append(child.Parents, parent)
		}
	}

	for _, node := range graph.Nodes {
		if !isDependency[node.key()] {
			node.Root = true
			graph.Roots = append(graph.Roots, node)
		}
	}

	slices.SortFunc(graph.Roots, func(a, b *GraphNode) int {
		if c := cmp.Compare(a.ID, b.ID); c != 0 {
			return c
		}
		return cmp.Compare(a.key(), b.key())
	})

	return graph, nil
}

// GetLeaves returns artifact nodes that declare no dependencies. A lock
// artifact is a leaf when none of the modules it ships has a dependency
// child; a probe artifact is a leaf when it has no dependency children.
// Being selected by a manifest requirement does not disqualify a package:
// leaves describe the dependency graph, not removability.
func (g *DependencyGraph) GetLeaves() []*GraphNode {
	var leaves []*GraphNode
	for _, node := range g.Nodes {
		if node.Module || g.artifactHasDependencies(node) {
			continue
		}
		leaves = append(leaves, node)
	}
	slices.SortFunc(leaves, func(a, b *GraphNode) int {
		if c := cmp.Compare(a.ID, b.ID); c != 0 {
			return c
		}
		return cmp.Compare(a.key(), b.key())
	})
	return leaves
}

// artifactHasDependencies reports whether an artifact node has dependency
// children, either through the modules it ships or, for a probe graph,
// directly through the artifact's own dependency edges.
func (g *DependencyGraph) artifactHasDependencies(node *GraphNode) bool {
	shipped := false
	for _, candidate := range g.Nodes {
		if !candidate.Module || candidate.Artifact != node.ID ||
			candidate.Environment != node.Environment ||
			candidate.Ecosystem != node.Ecosystem {
			continue
		}
		shipped = true
		if len(candidate.Children) > 0 {
			return true
		}
	}
	if shipped {
		return false
	}
	return len(node.Children) > 0
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
	slices.Sort(queue)

	result := make([]*GraphNode, 0, len(g.Nodes))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		result = append(result, g.Nodes[id])
		for _, parent := range g.Nodes[id].Parents {
			outDeg[parent.key()]--
			if outDeg[parent.key()] == 0 {
				queue = append(queue, parent.key())
			}
		}
	}

	if len(result) != len(g.Nodes) {
		return nil
	}
	return result
}

// OrderedRoots returns the root artifacts in dependency-first order, falling
// back to ID order when the graph has a cycle.
func (g *DependencyGraph) OrderedRoots() []*GraphNode {
	sorted := g.TopologicalSort()
	if sorted == nil {
		ordered := make([]*GraphNode, 0, len(g.Roots))
		ordered = append(ordered, g.Roots...)
		slices.SortFunc(ordered, func(a, b *GraphNode) int {
			if c := cmp.Compare(a.ID, b.ID); c != 0 {
				return c
			}
			return cmp.Compare(a.key(), b.key())
		})
		return ordered
	}

	position := make(map[string]int, len(sorted))
	for i, node := range sorted {
		position[node.key()] = i
	}
	ordered := make([]*GraphNode, 0, len(g.Roots))
	for _, root := range g.Roots {
		if _, ok := position[root.key()]; !ok {
			return nil
		}
		ordered = append(ordered, root)
	}
	slices.SortStableFunc(ordered, func(a, b *GraphNode) int {
		return cmp.Compare(position[a.key()], position[b.key()])
	})
	return ordered
}
