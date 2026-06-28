// Package modrinth provides functions to interact with Modrinth API.
//
// We use Modrinth terms in private functions:
//   - project: A project is a mod, plugin, or resource pack.
//   - Version: A version is a release, beta, or alpha version of a project.
//
// Generally, a project in Modrinth is equivalent to a project in Lucy. And
// a version in Modrinth is equivalent to a package in Lucy.
//
// Here, while referring to a project in lucy, we would try to the term "slug"
// to refer to the project (or it's name).
package modrinth

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"

	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

type provider struct{}

func (s provider) Search(q upstream.Query) (
	resp upstream.SearchResponse,
	err error,
) {
	var facets []facetItems
	switch q.FilterEcosystem {
	case types.EcoForge:
		facets = append(facets, facetForgeOnly)
	case types.EcoFabric:
		facets = append(facets, facetFabricOnly)
	case types.EcoNeoforge:
		facets = append(facets, facetNeoforgeOnly)
	case types.EcoBukkit:
		facets = append(facets, facetBukkitOnly)
	case types.EcoAny:
		fallthrough
	default:
		facets = append(facets, facetAllLoaders)
	}

	if q.ExcludeClient {
		facets = append(facets, facetServerSupported)
	}

	internalOptions := searchOptions{
		index:  "relevance",
		facets: facets,
	}
	searchUrl := searchUrl(q.Keyword, internalOptions)

	// Make the call to Modrinth API
	log.Debug("searching via modrinth api: " + searchUrl)
	res, err := requestBytes(searchUrl)
	if err != nil {
		return resp, fmt.Errorf("modrinth: search request failed: %w", err)
	}
	if res.StatusCode != 200 {
		return resp, fmt.Errorf(
			"%w: status %d",
			ErrInvalidAPIResponse,
			res.StatusCode,
		)
	}
	result := &searchResultResponse{}
	err = json.Unmarshal(res.Data, result)
	if err != nil {
		return resp, err
	}

	items := make([]upstream.SearchResult, len(result.Hits))
	for i, hit := range result.Hits {
		items[i] = upstream.SearchResult{
			RemoteName:  hit.Slug,
			Source:      s.Id(),
			Title:       hit.Title,
			Description: hit.Description,
			Downloads:   int64(hit.Downloads),
			LastUpdated: hit.DateModified,
		}
	}
	resp = upstream.SearchResponse{
		Source:   s.Id(),
		Items:    items,
		Warnings: nil,
	}

	return
}

func (s provider) Id() types.SourceId {
	return types.SourceModrinth
}

var Provider provider

func (s provider) Fetch(id types.VersionedPackageRef) (
	types.ResolvedPackage,
	error,
) {
	version, err := getVersion(id)
	if err != nil {
		return types.ResolvedPackage{}, err
	}
	if len(version.Files) == 0 || path.Ext(version.Files[0].Filename) != ".jar" {
		return types.ResolvedPackage{}, ErrUnsupportedFileType
	}
	resolved := resolvedPackageFromVersion(*version)
	resolved.Id.PackageRef = id.PackageRef
	resolved.Id.Version = id.Version
	return resolved, nil
}

func (s provider) Info(ref types.PackageRef) (types.Metadata, error) {
	project, err := getProjectByName(ref.Name)
	if err != nil {
		return types.Metadata{}, err
	}
	info := project.ToProjectInformation()
	info.From = s.Id()
	return info, nil
}

func (s provider) ListVersions(ref types.PackageRef) ([]upstream.VersionInfo, error) {
	versions, err := listVersions(ref.Name)
	if err != nil {
		return nil, err
	}

	infos := make([]upstream.VersionInfo, 0, len(versions))
	for _, version := range versions {
		if version == nil {
			continue
		}
		infos = append(infos, version.ToVersionInfo())
	}
	return infos, nil
}

var ErrInvalidAPIResponse = errors.New("received non-200 code from modrinth api")

// Temporary guard: Modrinth can ship non-JAR artifacts such as .mrpack,
// but Lucy does not support installing them yet.
var ErrUnsupportedFileType = errors.New("modrinth: only .jar files are supported")

func (s provider) Dependencies(
	id types.VersionedPackageRef,
) (*types.PackageDependencies, error) {
	version, err := getVersion(id)
	if err != nil {
		return nil, fmt.Errorf("modrinth: dependencies fetch failed: %w", err)
	}
	return new(
		(&modrinthDependencies{
			version:  version,
			platform: id.Eco,
		}).ToPackageDependencies(),
	), nil
}

func (s provider) ResolveVersionSelector(p types.VersionedPackageRef) (
	parsed types.VersionedPackageRef,
	err error,
) {
	if p.Eco.IsSelector() {
		// Platform inference removed to avoid circular imports.
		// Caller should provide explicit platform.
		p.Eco = types.EcoBare
	}
	parsed.Eco = p.Eco

	parsed.Name = p.Name

	var v *versionResponse

	switch p.Version {
	case types.VersionCompatible:
		v, err = latestCompatibleVersion(p.Name, p.Eco)
	case types.VersionAny, types.VersionNone, types.VersionLatest:
		v, err = latestVersion(p.Name)
	default:
		return p, nil
	}
	if err != nil {
		return p, err
	}
	parsed.Version = types.BareVersion(v.VersionNumber)

	return parsed, nil
}
