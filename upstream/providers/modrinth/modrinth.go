// Package modrinth provides functions to interact with Modrinth API.
//
// Terminology: a Modrinth project maps to a Lucy project, and a Modrinth
// version maps to a Lucy package version. Lucy-side names are Modrinth slugs;
// private helpers say "slug" for the project and "version" for a release,
// beta, or alpha.
package modrinth

import (
	"encoding/json/v2"
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
	case types.EcosystemForge:
		facets = append(facets, facetForgeOnly)
	case types.EcosystemFabric:
		facets = append(facets, facetFabricOnly)
	case types.EcosystemNeoForge:
		facets = append(facets, facetNeoforgeOnly)
	case types.EcosystemBukkit:
		facets = append(facets, facetBukkitOnly)
	case types.EcosystemUnspecified:
		fallthrough
	default:
		facets = append(facets, facetAllLoaders)
	}

	if !q.IncludeClient {
		facets = append(facets, facetServerSupported)
	}

	internalOptions := searchOptions{
		index:  "relevance",
		facets: facets,
	}
	searchUrl := searchUrl(q.Keyword, internalOptions)

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
			ProjectName: hit.Slug,
			Provider:    s.Provider(),
			Title:       hit.Title,
			Description: hit.Description,
			Downloads:   int64(hit.Downloads),
			LastUpdated: hit.DateModified,
		}
	}
	resp = upstream.SearchResponse{
		Provider: s.Provider(),
		Items:    items,
		Warnings: nil,
	}

	return
}

func (s provider) Provider() types.Provider {
	return types.ProviderModrinth
}

var Provider provider

func (s provider) Fetch(
	local upstream.LocalContext,
	id types.VersionedPackageRef,
) (types.ResolvedPackage, error) {
	version, err := getVersion(local, id)
	if err != nil {
		return types.ResolvedPackage{}, err
	}
	if len(version.Files) == 0 || path.Ext(version.Files[0].Filename) != ".jar" {
		return types.ResolvedPackage{}, ErrUnsupportedFileType
	}
	resolved := resolvedPackageFromVersion(*version)
	resolved.ID.PackageRef = id.PackageRef
	resolved.ID.Version = id.Version
	return resolved, nil
}

func (s provider) Info(ref types.PackageRef) (types.Metadata, error) {
	project, err := getProjectByName(ref.Name)
	if err != nil {
		return types.Metadata{}, err
	}
	info := project.ToProjectInformation()
	return info, nil
}

var ErrInvalidAPIResponse = errors.New("received non-200 code from modrinth api")

// Modrinth may publish non-JAR files; Lucy accepts only JAR artifacts.
var ErrUnsupportedFileType = errors.New("modrinth: only .jar files are supported")

func (s provider) Dependencies(
	local upstream.LocalContext,
	id types.VersionedPackageRef,
) (*types.DependencySet, error) {
	version, err := getVersion(local, id)
	if err != nil {
		return nil, fmt.Errorf("modrinth: dependencies fetch failed: %w", err)
	}
	return new(
		(&modrinthDependencies{
			version:  version,
			platform: id.Ecosystem,
		}).ToDependencySet(),
	), nil
}

func (s provider) ResolveVersionSelector(
	local upstream.LocalContext,
	p types.VersionedPackageRef,
) (parsed types.VersionedPackageRef, err error) {
	if p.Ecosystem.IsUnspecified() {
		p.Ecosystem = types.EcosystemUnspecified
	}
	parsed.Ecosystem = p.Ecosystem
	parsed.Name = p.Name

	var v *versionResponse
	switch p.Version {
	case types.VersionStable, types.VersionBeta, types.VersionAny:
		v, err = latestVersion(
			p.Name,
			p.Ecosystem,
			local.MinecraftVersion,
			p.Version,
		)
	default:
		return p, nil
	}
	if err != nil {
		return p, err
	}
	parsed.Version = types.Version(v.VersionNumber)
	return parsed, nil
}
