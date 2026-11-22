package resolve

import (
	"slices"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
)

// lockedEdges restores only the relationships the lock selected from those
// published upstream. In particular, an imported pack can use a different
// provider or omit a client-only dependency. Only an additive change can
// reuse that graph. Changed or removed bindings require fresh expansion.
func (l lockIndex) lockedEdges(requests []projectRequest, packages []document.Package) map[projectKey][]projectRequest {
	matched := 0
	for _, r := range requests {
		binding := r.requirements[0]
		key := r.key
		key.project = binding.Reference
		if p := l.byReference[key]; p != nil {
			if !slices.Contains(p.Requirements, binding) {
				return nil
			}
			matched++
		}
	}
	if matched != len(l.byReference) {
		return nil
	}
	// DependencyOf does not carry the parent's ecosystem. If two parent
	// selections have the same reference, reconstruct neither by guessing.
	parents := make(map[projectKey]projectKey, len(packages))
	for key := range l.byProject {
		ref := key
		ref.ecosystem = catalog.EcosystemUnspecified
		if _, exists := parents[ref]; exists {
			return nil
		}
		parents[ref] = key
	}
	edges := make(map[projectKey][]projectRequest)
	for i := range packages {
		p := &packages[i]
		for _, ref := range p.DependencyOf {
			parent, ok := parents[projectKey{p.Environment, catalog.EcosystemUnspecified, ref.Provider, string(ref.Name)}]
			if !ok {
				return nil
			}
			child := projectRequest{
				key:          projectKey{p.Environment, p.Ecosystem, p.Provider, p.ProjectID},
				origin:       originDependency,
				dependencyOf: ref,
			}
			if p.ReleaseID != "" {
				child.releaseHints = []releaseHint{{releaseID: p.ReleaseID}}
			}
			edges[parent] = append(edges[parent], child)
		}
	}
	return edges
}
