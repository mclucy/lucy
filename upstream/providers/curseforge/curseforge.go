// Package curseforge provides functions to interact with CurseForge API.
//
// CurseForge identifies mods by numeric modId, not by slug. Slug resolution
// is done via the search endpoint with the slug query parameter.
//
// All API requests require an x-api-key header. The key is injected at build
// time via ldflags into the ApiKey variable.
package curseforge

import (
	"fmt"

	"github.com/mclucy/lucy/logger"
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
	options := types.SearchOptions{
		IncludeClient:  !q.ExcludeClient,
		SortBy:         q.SortBy,
		FilterPlatform: q.FilterPlatform,
	}
	u := searchUrl(types.BarePackageName(q.Keyword), options)
	logger.Debug("searching via curseforge api: " + u)

	resp := &searchResponse{}
	if err := get(u, resp); err != nil {
		return upstream.SearchResponse{}, err
	}
	return resp.ToSearchResults(p.Id()), nil
}

// Fetch resolves the package version, then fetches the corresponding file.
func (p provider) Fetch(id types.VersionedPackageRef) (
	remote upstream.RawPackageRemote,
	err error,
) {
	mod, err := resolveSlug(id.Name)
	if err != nil {
		return nil, err
	}

	file, err := getFileByDisplayName(mod.Id, string(id.Version), id.Platform)
	if err != nil {
		return nil, err
	}

	return file, nil
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
	info := rawProjectInformation{mod: mod, description: description}.ToProjectInformation()
	info.From = p.Id()
	return info, nil
}

func (p provider) Dependencies(
	id types.VersionedPackageRef,
) (deps upstream.RawPackageDependencies, err error) {
	// Resolve the mod to get the modId
	mod, err := resolveSlug(id.Name)
	if err != nil {
		return nil, err
	}

	// Get the specific file matching the version
	file, err := getFileByDisplayName(mod.Id, string(id.Version), id.Platform)
	if err != nil {
		return nil, err
	}

	// If no specific version, get latest release
	if file == nil {
		file, err = latestCompatibleFile(mod.Id, id.Platform)
		if err != nil {
			return nil, err
		}
	}

	return &curseforgeDependencies{file: file}, nil
}

// curseforgeDependencies wraps a fileResponse for dependency
// normalization. It implements upstream.RawPackageDependencies.
type curseforgeDependencies struct {
	file *fileResponse
}

var _ upstream.RawPackageDependencies = (*curseforgeDependencies)(nil)

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
						Name: types.BarePackageName(
							fmt.Sprintf(
								"%d",
								dep.ModId,
							),
						),
					},
					Mandatory: false,
				},
			)
		case 3: // RequiredDependency
			result.Value = append(
				result.Value, types.Dependency{
					Id: types.VersionedPackageRef{
						Name: types.BarePackageName(
							fmt.Sprintf(
								"%d",
								dep.ModId,
							),
						),
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

func (provider) Support(
	name types.BarePackageName,
) (supports upstream.RawProjectSupport, err error) {
	panic("TODO: implement curseforge provider Support")
}

// ResolveVersionSelector resolves abstract version specifiers (latest,
// compatible, any) to a concrete version by querying the CurseForge API.
func (p provider) ResolveVersionSelector(id types.VersionedPackageRef) (
	parsed types.VersionedPackageRef,
	err error,
) {
	if id.Platform.IsSelector() {
		// Platform inference removed to avoid circular imports.
		// Caller should provide explicit platform.
		id.Platform = types.PlatformNone
	}
	parsed.Platform = id.Platform

	parsed.Name = id.Name

	var file *fileResponse

	switch id.Version {
	case types.VersionCompatible:
		mod, err := resolveSlug(id.Name)
		if err != nil {
			return id, err
		}
		file, err = latestCompatibleFile(mod.Id, id.Platform)
		if err != nil {
			return id, err
		}
	case types.VersionAny, types.VersionNone, types.VersionLatest:
		mod, err := resolveSlug(id.Name)
		if err != nil {
			return id, err
		}
		file, err = latestFile(mod.Id)
		if err != nil {
			return id, err
		}
	default:
		return id, nil
	}

	parsed.Version = types.BareVersion(file.FileName)
	return parsed, nil
}
