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

func (provider) Id() types.SourceId {
	return types.SourceCurseForge
}

// Search queries the CurseForge /v1/mods/search endpoint.
func (p provider) Search(q upstream.Query) (upstream.SearchResponse, error) {
	options := upstream.SearchOptions{
		IncludeClient:   !q.ExcludeClient,
		FilterEcosystem: q.FilterEcosystem,
	}
	u := searchUrl(types.BarePackageName(q.Keyword), options)
	log.Debug("searching via curseforge api: " + u)

	resp := &searchResponse{}
	if err := get(u, resp); err != nil {
		return upstream.SearchResponse{}, err
	}
	return resp.ToSearchResults(p.Id()), nil
}

// Fetch resolves the package version, then fetches the corresponding file.
func (p provider) Fetch(
	local upstream.LocalContext,
	id types.VersionedPackageRef,
) (types.ResolvedPackage, error) {
	mod, err := resolveSlug(id.Name)
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	file, err := getFileByDisplayName(mod.Id, string(id.Version), local, id.Eco)
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	resolved := file.ToPackageRemote()
	resolved.Id.PackageRef = id.PackageRef
	resolved.Id.Version = id.Version
	return resolved, nil
}

// Info resolves a project slug and returns project metadata.
func (p provider) Info(ref types.PackageRef) (types.Metadata, error) {
	mod, err := resolveSlug(ref.Name)
	if err != nil {
		return types.Metadata{}, err
	}
	description, err := getModDescription(mod.Id)
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
) (*types.PackageDependencies, error) {
	mod, err := resolveSlug(id.Name)
	if err != nil {
		return nil, err
	}

	file, err := getFileByDisplayName(mod.Id, string(id.Version), local, id.Eco)
	if err != nil {
		return nil, err
	}

	return new((&curseforgeDependencies{file: file}).ToPackageDependencies()), nil
}

// curseforgeDependencies wraps a fileResponse for dependency
// normalization.
type curseforgeDependencies struct {
	file *fileResponse
}

func (c *curseforgeDependencies) ToPackageDependencies() types.PackageDependencies {
	result := types.PackageDependencies{
		Authentic: false,
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
		case 2: // OptionalDependency
			result.Value = append(
				result.Value, types.Dependency{
					Id: types.VersionedPackageRef{
						PackageRef: types.PackageRef{
							Name: types.BarePackageName(
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
		case 3: // RequiredDependency
			result.Value = append(
				result.Value, types.Dependency{
					Id: types.VersionedPackageRef{
						PackageRef: types.PackageRef{
							Name: types.BarePackageName(
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
			// Skip 1, 4, 5, 6 - not runtime dependencies
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
	if id.Eco.IsSelector() {
		id.Eco = types.EcoUnspecified
	}
	parsed.Eco = id.Eco
	parsed.Name = id.Name

	mod, err := resolveSlug(id.Name)
	if err != nil {
		return id, err
	}

	var file *fileResponse
	switch id.Version {
	case types.VersionStable:
		file, err = latestCompatibleFile(mod.Id, local, id.Eco)
	case types.VersionBeta, types.VersionAny:
		file, err = latestFile(mod.Id, local, id.Eco)
	default:
		return id, nil
	}
	if err != nil {
		return id, err
	}

	parsed.Version = types.BareVersion(file.FileName)
	return parsed, nil
}
