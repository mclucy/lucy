package cli

import (
	"slices"
	"sort"

	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/workspace"
)

// GraphNode is one node of a resolved package graph. A node is either an
// artifact coordinate ("provider:project", taken from the lock's explicit
// provider fields) or a native module (a module ID declared inside an
// artifact). Identity is always an explicit field; nothing is recovered by
// parsing a display string.
type GraphNode struct {
	// ID is the native module ID, or "provider:project" for an artifact.
	ID string

	Version string

	// Provider and ProjectID carry the upstream coordinates of an artifact
	// node. They are empty for a native module node, which belongs to the
	// artifact recorded in Artifact.
	Provider  string
	ProjectID string

	// Artifact is the coordinate node that ships this module. It is empty
	// for artifact nodes themselves.
	Artifact string

	Filename string

	// Runtime scopes the node ("server" or "mcdr") and Loader scopes it
	// within that runtime. Dependency edges only resolve inside one scope.
	Runtime string
	Loader  types.Ecosystem

	// Module reports whether the node is a native module rather than an
	// artifact coordinate.
	Module bool

	// Core reports a node that is not a removable package. Server core and
	// bootstrap inputs are never graph packages, so this marks nodes that
	// requirement.
	Root bool

	Children []*GraphNode

	Parents []*GraphNode

	// InDegree is populated after graph build: the number of parents.
	InDegree int

	// dependedOn records that another package requires this artifact.
	dependedOn bool
}

type DependencyGraph struct {
	// Nodes is keyed by the scoped node key, so identically named modules
	// in different runtimes or loaders stay distinct.
	Nodes map[string]*GraphNode

	// Roots are the artifacts an enabled manifest requirement selects.
	Roots []*GraphNode
}

// nodeKey scopes an identity so edges never cross runtimes or loaders.
func nodeKey(runtime string, loader types.Ecosystem, id string) string {
	return runtime + "|" + string(loader) + "|" + id
}

// artifactCoordinate renders an artifact's explicit provider coordinates.
func artifactCoordinate(provider, projectID string) string {
	return provider + ":" + projectID
}

// BuildGraphFromLock builds the graph from a lock's resolved packages.
//
// Roots are the artifacts selected by an enabled manifest requirement.
// Module dependency edges come from each artifact's native module
// descriptors. The server core and bootstrap inputs are lock inputs, not
// packages, so they never become graph nodes and therefore never appear as
// removable leaves.
func BuildGraphFromLock(lock *lockfile.Document) (*DependencyGraph, error) {
	graph := &DependencyGraph{
		Nodes: make(map[string]*GraphNode),
	}

	// owners maps a scoped module key to the artifact node shipping it, so a
	// dependency on a module can also mark that artifact as depended upon.
	owners := make(map[string]*GraphNode)
	dependencies := make(map[string][]types.NativeDependency)

	for _, pkg := range lock.Packages {
		coordinate := artifactCoordinate(pkg.Provider, pkg.ProjectID)
		key := nodeKey(pkg.Runtime, pkg.Loader, coordinate)

		node, ok := graph.Nodes[key]
		if !ok {
			node = &GraphNode{
				ID:        coordinate,
				Version:   pkg.Version,
				Provider:  pkg.Provider,
				ProjectID: pkg.ProjectID,
				Filename:  pkg.Filename,
				Runtime:   pkg.Runtime,
				Loader:    pkg.Loader,
			}
			graph.Nodes[key] = node
		}

		for _, module := range pkg.Modules {
			moduleKey := nodeKey(pkg.Runtime, pkg.Loader, module.ID)
			if _, exists := graph.Nodes[moduleKey]; !exists {
				graph.Nodes[moduleKey] = &GraphNode{
					ID:       module.ID,
					Version:  module.Version,
					Artifact: coordinate,
					Runtime:  pkg.Runtime,
					Loader:   pkg.Loader,
					Module:   true,
					Filename: pkg.Filename,
				}
			}
			owners[moduleKey] = node
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
				node.Runtime == root.Runtime && node.Loader == root.Loader {
				root.Children = append(root.Children, node)
				node.Parents = append(node.Parents, root)
			}
		}
	}

	// Dependency edges stay inside one runtime/loader scope.
	for _, node := range graph.Nodes {
		if !node.Module {
			continue
		}
		for _, dep := range dependencies[node.key()] {
			child := resolveDependencyNode(graph, node.Runtime, node.Loader, dep)
			if child == nil || child == node {
				continue
			}
			if !slices.Contains(child.Parents, node) {
				node.Children = append(node.Children, child)
				child.Parents = append(child.Parents, node)
			}
			if owner, ok := owners[child.key()]; ok {
				owner.markDepended()
			}
		}
	}

	for _, node := range graph.Nodes {
		if node.Module {
			node.InDegree = len(node.Parents)
			continue
		}
		if node.dependedOn {
			node.InDegree = 1
		}
	}

	sort.Slice(graph.Roots, func(i, j int) bool {
		return graph.Roots[i].ID < graph.Roots[j].ID
	})
	return graph, nil
}

func (n *GraphNode) key() string {
	return nodeKey(n.Runtime, n.Loader, n.ID)
}

// markDepended records that another package requires this artifact. Artifact
// nodes carry no tree edges of their own, so dependency bookkeeping is
// tracked separately from the rendered parent/child relationships.
func (n *GraphNode) markDepended() {
	n.dependedOn = true
}

// resolveDependencyNode maps a declared dependency onto a graph node. A
// native dependency names a module ID; when it instead carries upstream
// project coordinates, it resolves to the artifact that ships them.
func resolveDependencyNode(
	graph *DependencyGraph,
	runtime string,
	loader types.Ecosystem,
	dep types.NativeDependency,
) *GraphNode {
	if dep.ModuleID != "" {
		if node, ok := graph.Nodes[nodeKey(runtime, loader, dep.ModuleID)]; ok {
			return node
		}
	}
	if dep.Provider != "" && dep.ProjectID != "" {
		coordinate := artifactCoordinate(dep.Provider, dep.ProjectID)
		if node, ok := graph.Nodes[nodeKey(runtime, loader, coordinate)]; ok {
			return node
		}
	}
	if dep.ModuleID == "" && dep.ID != "" {
		if node, ok := graph.Nodes[nodeKey(runtime, loader, dep.ID)]; ok {
			return node
		}
	}
	return nil
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
			Loader:  p.Id.Eco,
		}
		graph.Nodes[nodeKey(RuntimeServer, p.Id.Eco, id)] = node
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

			parent, parentOK := graph.Nodes[nodeKey(RuntimeServer, p.Id.Eco, parentID)]
			child, childOK := graph.Nodes[nodeKey(RuntimeServer, p.Id.Eco, depID)]

			if parentOK && childOK {
				parent.Children = append(parent.Children, child)
				child.Parents = append(child.Parents, parent)
			}
		}
	}

	for _, node := range graph.Nodes {
		if !isDependency[node.ID] {
			node.Root = true
			graph.Roots = append(graph.Roots, node)
		}
	}

	for _, node := range graph.Nodes {
		node.InDegree = len(node.Parents)
	}

	return graph, nil
}

// GetLeaves returns removable packages: artifact nodes that no enabled
// requirement selects and that nothing else depends upon, sorted by ID.
// Module nodes are never leaves. The server core and bootstrap inputs are
// lock inputs rather than packages, so they never become nodes and can
// never be reported as removable.
func (g *DependencyGraph) GetLeaves() []*GraphNode {
	var leaves []*GraphNode
	for _, node := range g.Nodes {
		if node.Module || node.Root || node.InDegree != 0 {
			continue
		}
		leaves = append(leaves, node)
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
	sort.Strings(queue)

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
		sort.Slice(ordered, func(i, j int) bool {
			return ordered[i].ID < ordered[j].ID
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
	sort.SliceStable(ordered, func(i, j int) bool {
		return position[ordered[i].key()] < position[ordered[j].key()]
	})
	return ordered
}

// RuntimeServer and RuntimeMCDR are the lock runtime names that scope graph
// nodes and lock packages.
const (
	RuntimeServer = "server"
	RuntimeMCDR   = "mcdr"
)
