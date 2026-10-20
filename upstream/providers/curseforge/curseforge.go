// Package curseforge provides functions to interact with CurseForge API.
//
// CurseForge identifies mods by numeric modId, not by slug. Slug resolution
// is done via the search endpoint with the slug query parameter.
//
// All API requests require an x-api-key header. The key is decoded on first use
// from linker-injected cipher fragments.
package curseforge

import (
	"fmt"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

type provider struct{}

var Provider provider

func (provider) Provider() types.Provider {
	return types.ProviderCurseForge
}

func (p provider) Search(q upstream.Query) (upstream.SearchResponse, error) {
	// CurseForge exposes no client/server search facet; IncludeClient cannot
	// be applied upstream and is left to post-filtering.
	u := searchUrl(types.PackageName(q.Keyword), q.FilterEcosystem)
	log.Debug("searching via curseforge api: " + u)

	resp := &searchResponse{}
	if err := get(u, resp); err != nil {
		return upstream.SearchResponse{}, err
	}
	return resp.ToSearchResults(p.Provider()), nil
}

func (p provider) Fetch(
	local upstream.LocalContext,
	id types.VersionedPackageRef,
) (types.ResolvedPackage, error) {
	mod, err := resolveSlug(id.Name)
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	file, err := getFileByDisplayName(mod.ID, string(id.Version), local, id.Ecosystem)
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	resolved := file.ToPackageRemote()
	resolved.ID.PackageRef = id.PackageRef
	resolved.ID.Version = id.Version
	return resolved, nil
}

func (p provider) Info(ref types.PackageRef) (types.Metadata, error) {
	mod, err := resolveSlug(ref.Name)
	if err != nil {
		return types.Metadata{}, err
	}
	description, err := getModDescription(mod.ID)
	if err != nil {
		return types.Metadata{}, err
	}
	info := rawProjectInformation{
		mod: mod, description: description,
	}.ToProjectInformation()
	return info, nil
}

func (p provider) Dependencies(
	local upstream.LocalContext,
	id types.VersionedPackageRef,
) (*types.DependencySet, error) {
	mod, err := resolveSlug(id.Name)
	if err != nil {
		return nil, err
	}

	file, err := getFileByDisplayName(mod.ID, string(id.Version), local, id.Ecosystem)
	if err != nil {
		return nil, err
	}

	return new((&curseforgeDependencies{file: file}).ToDependencySet()), nil
}

// curseforgeDependencies wraps a fileResponse for dependency
// normalization.
type curseforgeDependencies struct {
	file *fileResponse
}

func (c *curseforgeDependencies) ToDependencySet() types.DependencySet {
	result := types.DependencySet{
		FromArtifact: false,
	}

	for _, dep := range c.file.Dependencies {
		// relationType mapping:
		// 1 = EmbeddedLibrary (skip - embedded in the mod itself)
		// 2 = OptionalDependency -> Mandatory: false
		// 3 = RequiredDependency -> Mandatory: true
		// 4 = Tool (skip - not a runtime dependency)
		// 5 = Incompatible (skip - breaks compatibility)
		// 6 = Include (skip - bundled with the mod)

		switch dep.RelationType {
		case 2:
			result.Dependencies = append(
				result.Dependencies, types.Dependency{
					ID: types.VersionedPackageRef{
						PackageRef: types.PackageRef{
							Name: types.PackageName(
								fmt.Sprintf(
									"%d",
									dep.ModId,
								),
							),
						},
					},
					Mandatory: false,
				},
			)
		case 3:
			result.Dependencies = append(
				result.Dependencies, types.Dependency{
					ID: types.VersionedPackageRef{
						PackageRef: types.PackageRef{
							Name: types.PackageName(
								fmt.Sprintf(
									"%d",
									dep.ModId,
								),
							),
						},
					},
					Mandatory: true,
				},
			)
		default:
			continue
		}
	}

	return result
}

// ResolveVersionSelector resolves abstract version specifiers (any, stable,
// beta) to a concrete version by querying the CurseForge API.
func (p provider) ResolveVersionSelector(
	local upstream.LocalContext,
	id types.VersionedPackageRef,
) (parsed types.VersionedPackageRef, err error) {
	if id.Ecosystem.IsUnspecified() {
		id.Ecosystem = types.EcosystemUnspecified
	}
	parsed.Ecosystem = id.Ecosystem
	parsed.Name = id.Name

	mod, err := resolveSlug(id.Name)
	if err != nil {
		return id, err
	}

	var file *fileResponse
	switch id.Version {
	case types.VersionStable, types.VersionBeta, types.VersionAny:
		file, err = latestFile(mod.ID, local, id.Ecosystem, id.Version)
	default:
		return id, nil
	}
	if err != nil {
		return id, err
	}

	parsed.Version = types.Version(file.FileName)
	return parsed, nil
}
