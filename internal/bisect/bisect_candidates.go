package bisect

import (
	"context"
	"slices"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
	"github.com/mclucy/lucy/internal/depgraph"
	"github.com/mclucy/lucy/server"
	"github.com/mclucy/lucy/workspace/identify"
)

// selectCandidates returns the manifest requirements a session may toggle,
// ordered so a package always follows the packages it depends on. doc is the
// recovered snapshot the session records as its original; dependency loading
// may take long enough that start must revalidate the file before publishing.
func selectCandidates(
	ctx context.Context,
	lookup identify.HashLookup,
	workDir string,
	doc *document.Manifest,
) ([]Mod, depgraph.Findings, error) {
	loaded, err := depgraph.Load(ctx, workDir, false, lookup)
	if err != nil {
		return nil, loaded.Findings, err
	}

	var mods []Mod
	seen := make(map[string]bool)
	for _, root := range loaded.Graph.OrderedArtifacts() {
		for _, reference := range nodeReferences(doc, root) {
			if !requirementEnabled(
				doc,
				root.Environment,
				root.Ecosystem,
				reference,
			) {
				continue
			}
			mod := Mod{
				Environment: root.Environment,
				Ecosystem:   root.Ecosystem,
				Reference:   reference,
				Version:     root.Version,
			}
			key := string(mod.Environment) + "|" +
				string(mod.Ecosystem) + "|" + mod.Reference
			if seen[key] {
				continue
			}
			seen[key] = true
			mods = append(mods, mod)
		}
	}
	return mods, loaded.Findings, nil
}

// nodeReferences returns the manifest requirement references that select
// node. A lock graph carries the exact bindings on the node, including several
// bindings for one artifact. An observation graph has no lock bindings, so the
// manifest references are matched by provider and project identity.
func nodeReferences(doc *document.Manifest, node *depgraph.Node) []string {
	if len(node.Requirements) > 0 {
		references := slices.Clone(node.Requirements)
		slices.Sort(references)
		return slices.Compact(references)
	}
	var packages map[string]document.Requirement
	switch node.Environment {
	case server.EnvironmentServer:
		packages = doc.Server.Packages[node.Ecosystem]
	case server.EnvironmentMCDR:
		if doc.MCDR != nil {
			packages = doc.MCDR.Packages
		}
	}
	references := make([]string, 0, len(packages))
	for reference := range packages {
		if referenceMatchesNode(reference, node) {
			references = append(references, reference)
		}
	}
	slices.Sort(references)
	return references
}

func referenceMatchesNode(reference string, node *depgraph.Node) bool {
	provider, project, err := document.ParseReference(reference)
	if err != nil {
		return false
	}
	if node.Provider != catalog.ProviderUnspecified && provider != node.Provider {
		return false
	}
	if node.ProjectID != "" {
		return project == node.ProjectID
	}
	if node.Provider != catalog.ProviderUnspecified {
		return strings.TrimPrefix(node.ID, node.Provider.String()+":") == project
	}
	return node.ID == project
}

// requirementEnabled reports whether the manifest still enables the given
// requirement entry.
func requirementEnabled(
	doc *document.Manifest,
	environment server.Environment,
	ecosystem catalog.Ecosystem,
	reference string,
) bool {
	var packages map[string]document.Requirement
	switch environment {
	case server.EnvironmentServer:
		if doc.Server.Packages == nil {
			return false
		}
		packages = doc.Server.Packages[ecosystem]
	case server.EnvironmentMCDR:
		if doc.MCDR == nil {
			return false
		}
		packages = doc.MCDR.Packages
	default:
		return false
	}
	requirement, ok := packages[reference]
	return ok && requirement.IsEnabled()
}
