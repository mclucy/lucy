package depgraph

import (
	"slices"
)

// Leaves returns artifact nodes that declare no dependencies. A lock
// artifact is a leaf when none of the modules it ships has a dependency
// child; a probe artifact is a leaf when it has no dependency children.
// Being selected by a manifest requirement does not disqualify a package:
// leaves describe the dependency graph, not removability.
func (g *Graph) Leaves() []*Node {
	var leaves []*Node
	for _, node := range g.Nodes {
		if node.Module || g.artifactHasDependencies(node) {
			continue
		}
		leaves = append(leaves, node)
	}
	sortNodes(leaves)
	return leaves
}

// artifactHasDependencies reports whether an artifact node has dependency
// children, either through the modules it ships or, for a probe graph,
// directly through the artifact's own dependency edges.
func (g *Graph) artifactHasDependencies(node *Node) bool {
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

// TopologicalSort returns nodes in dependency-first order (dependencies
// before dependents). It returns nil if a cycle is detected.
func (g *Graph) TopologicalSort() []*Node {
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

	result := make([]*Node, 0, len(g.Nodes))
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		result = append(result, g.Nodes[id])
		for _, parent := range g.Nodes[id].Parents {
			outDeg[parent.Key()]--
			if outDeg[parent.Key()] == 0 {
				queue = append(queue, parent.Key())
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
func (g *Graph) OrderedRoots() []*Node {
	sorted := g.TopologicalSort()
	if sorted == nil {
		ordered := make([]*Node, 0, len(g.Roots))
		ordered = append(ordered, g.Roots...)
		sortNodes(ordered)
		return ordered
	}

	position := make(map[string]int, len(sorted))
	for i, node := range sorted {
		position[node.Key()] = i
	}
	ordered := make([]*Node, 0, len(g.Roots))
	for _, root := range g.Roots {
		if _, ok := position[root.Key()]; !ok {
			return nil
		}
		ordered = append(ordered, root)
	}
	slices.SortStableFunc(ordered, func(a, b *Node) int {
		return position[a.Key()] - position[b.Key()]
	})
	return ordered
}

// OrderedArtifacts returns every artifact node in dependency-first order.
// Root selection is not enough: in a live observation, a directly declared
// requirement can also be another package's dependency.
//
// The order is computed on the artifact graph. Sorting the mixed graph and
// dropping modules afterwards is not enough: an artifact only becomes ready
// after every module it ships, while a dependent only waits for the module it
// needs, so a package shipping several modules can follow its own dependent.
// Collapsing each module onto its artifact first keeps the edge from that
// dependent to the package that provides the required module.
func (g *Graph) OrderedArtifacts() []*Node {
	artifacts := make([]*Node, 0, len(g.Nodes))
	for _, node := range g.Nodes {
		if !node.Module {
			artifacts = append(artifacts, node)
		}
	}
	byID := func() []*Node {
		sortNodes(artifacts)
		return artifacts
	}

	// A module is represented by the artifact that ships it.
	owner := make(map[string]*Node, len(g.Nodes))
	for _, node := range g.Nodes {
		if !node.Module {
			owner[node.Key()] = node
			continue
		}
		artifact, ok := g.Nodes[nodeKey(
			node.Environment,
			node.Ecosystem,
			node.Artifact,
		)]
		if ok {
			owner[node.Key()] = artifact
		}
	}

	// Lift edges onto owning artifacts, dropping the tree-only
	// artifact-to-own-module self edges.
	dependencies := make(map[string]map[string]bool, len(artifacts))
	dependents := make(map[string][]*Node, len(artifacts))
	for _, node := range g.Nodes {
		dependent := owner[node.Key()]
		if dependent == nil {
			continue
		}
		dependentKey := dependent.Key()
		for _, child := range node.Children {
			dependency := owner[child.Key()]
			if dependency == nil || dependency == dependent {
				continue
			}
			dependencyKey := dependency.Key()
			if dependencies[dependentKey] == nil {
				dependencies[dependentKey] = make(map[string]bool)
			}
			if dependencies[dependentKey][dependencyKey] {
				continue
			}
			dependencies[dependentKey][dependencyKey] = true
			dependents[dependencyKey] = append(dependents[dependencyKey], dependent)
		}
	}

	remaining := make(map[string]int, len(artifacts))
	queue := make([]*Node, 0, len(artifacts))
	for _, artifact := range artifacts {
		key := artifact.Key()
		remaining[key] = len(dependencies[key])
		if remaining[key] == 0 {
			queue = append(queue, artifact)
		}
	}
	sortNodes(queue)

	// Each artifact enters the queue at most once, so insertion reuses the
	// preallocated capacity.
	ordered := make([]*Node, 0, len(artifacts))
	for head := 0; head < len(queue); head++ {
		artifact := queue[head]
		ordered = append(ordered, artifact)
		for _, dependent := range dependents[artifact.Key()] {
			dependentKey := dependent.Key()
			remaining[dependentKey]--
			if remaining[dependentKey] != 0 {
				continue
			}
			index, _ := slices.BinarySearchFunc(
				queue[head+1:],
				dependent,
				compareNodes,
			)
			queue = slices.Insert(queue, head+1+index, dependent)
		}
	}
	if len(ordered) != len(artifacts) {
		return byID()
	}
	return ordered
}
