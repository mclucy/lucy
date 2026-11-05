// Package depgraph models a resolved package graph and traverses it. It
// returns graph facts only; commands own presentation.
package depgraph

import (
	"cmp"
	"path/filepath"
	"slices"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/workspace"
)

// Node is one node of a resolved package graph. A node is either an artifact
// coordinate ("provider:project", taken from the lock's explicit provider
// fields) or a descriptor module (a module ID declared inside an artifact).
// Identity is always an explicit field; nothing is recovered by parsing a
// display string.
type Node struct {
	// ID is the native module ID, or "provider:project" for an artifact.
	ID string

	Version string

	// Provider and ProjectID carry the upstream coordinates of an artifact
	// node. They are empty for a native module node, which belongs to the
	// artifact recorded in Artifact.
	Provider  catalog.Provider
	ProjectID string
	// Requirements lists the manifest requirement references bound to this
	// artifact. One artifact can satisfy several declared references.
	Requirements []string

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

	Children []*Node

	Parents []*Node
}

// Graph is one resolved package universe. Nodes are unique by scoped
// identity, and Roots are the artifacts an enabled requirement selects.
type Graph struct {
	// Nodes are keyed by the scoped node key, so identically named modules
	// in different environments or ecosystems stay distinct.
	Nodes map[string]*Node

	// Roots are the artifacts an enabled manifest requirement selects.
	Roots []*Node
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

// Key returns the scoped identity used to address the node within a graph.
func (n *Node) Key() string {
	return nodeKey(n.Environment, n.Ecosystem, n.ID)
}

// artifactCoordinate renders an artifact's explicit provider coordinates.
func artifactCoordinate(provider catalog.Provider, projectID string) string {
	return provider.String() + ":" + projectID
}

// BuildFromLock builds the graph from a lock's resolved packages.
//
// Roots are the artifacts selected by an enabled manifest requirement.
// Module dependency edges come from each artifact's native module
// descriptors. The server core and bootstrap inputs are lock inputs, not
// packages, so they never become graph nodes.
func BuildFromLock(lock *document.Lock) (*Graph, error) {
	graph := &Graph{
		Nodes: make(map[string]*Node),
	}

	dependencies := make(map[string][]catalog.DescriptorDependency)

	for _, pkg := range lock.Packages {
		coordinate := artifactCoordinate(pkg.Provider, pkg.ProjectID)
		key := nodeKey(pkg.Environment, pkg.Ecosystem, coordinate)

		node, ok := graph.Nodes[key]
		if !ok {
			node = &Node{
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
				graph.Nodes[moduleKey] = &Node{
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
			if binding.Reference != "" &&
				!slices.Contains(node.Requirements, binding.Reference) {
				node.Requirements = append(node.Requirements, binding.Reference)
			}
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
		for _, dep := range dependencies[node.Key()] {
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

	sortNodes(graph.Roots)
	return graph, nil
}

// resolveDependencyNode maps a declared dependency onto a graph node. A
// native dependency names a module ID; when it instead carries upstream
// project coordinates, it resolves to the artifact that ships them.
func resolveDependencyNode(
	graph *Graph,
	environment server.Environment,
	ecosystem catalog.Ecosystem,
	dep catalog.DescriptorDependency,
) *Node {
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

// BuildFromObservation builds the graph from an observed server's packages.
// A package that no other package lists as a dependency is a root;
// parent-child relationships come from each package's Dependencies field.
func BuildFromObservation(info *workspace.Workspace) (*Graph, error) {
	graph := &Graph{
		Nodes: make(map[string]*Node),
	}

	for _, p := range info.Packages {
		id := p.ID.StringBase()
		node := &Node{
			ID:          id,
			Version:     p.ID.Version.String(),
			Provider:    p.ID.Provider,
			ProjectID:   p.ID.Name.String(),
			Filename:    filepath.Base(p.Path),
			Environment: server.EnvironmentServer,
			Ecosystem:   p.ID.Ecosystem,
		}
		graph.Nodes[node.Key()] = node
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
			isDependency[child.Key()] = true

			if slices.Contains(parent.Children, child) {
				continue
			}
			parent.Children = append(parent.Children, child)
			child.Parents = append(child.Parents, parent)
		}
	}

	for _, node := range graph.Nodes {
		if !isDependency[node.Key()] {
			node.Root = true
			graph.Roots = append(graph.Roots, node)
		}
	}

	sortNodes(graph.Roots)
	return graph, nil
}

// sortNodes orders nodes by identifier, then by scoped identity.
func sortNodes(nodes []*Node) {
	slices.SortFunc(nodes, compareNodes)
}

func compareNodes(a, b *Node) int {
	if c := cmp.Compare(a.ID, b.ID); c != 0 {
		return c
	}
	return cmp.Compare(a.Key(), b.Key())
}
