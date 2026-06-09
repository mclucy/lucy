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
	"io"
	"net/http"
	"path"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

type provider struct{}

func (s provider) Search(q upstream.Query) (
	resp upstream.SearchResponse,
	err error,
) {
	var facets []facetItems
	switch q.FilterPlatform {
	case types.PlatformForge:
		facets = append(facets, facetForgeOnly)
	case types.PlatformFabric:
		facets = append(facets, facetFabricOnly)
	case types.PlatformNeoforge:
		facets = append(facets, facetNeoforgeOnly)
	case types.PlatformBukkit:
		facets = append(facets, facetBukkitOnly)
	case types.PlatformAny:
		fallthrough
	default:
		facets = append(facets, facetAllLoaders)
	}

	if q.ExcludeClient {
		facets = append(facets, facetServerSupported)
	}

	internalOptions := searchOptions{
		index:  modrinthSearchSortingString(q.SortBy),
		facets: facets,
	}
	searchUrl := searchUrl(q.Keyword, internalOptions)

	// Make the call to Modrinth API
	logger.Debug("searching via modrinth api: " + searchUrl)
	httpRes, err := http.Get(searchUrl)
	if err != nil {
		return resp, fmt.Errorf("modrinth: search request failed: %w", err)
	}
	defer tools.CloseReader(httpRes.Body, logger.Warn)
	if httpRes.StatusCode != http.StatusOK {
		return resp, fmt.Errorf("%w: %s", ErrInvalidAPIResponse, httpRes.Status)
	}

	data, err := io.ReadAll(httpRes.Body)
	if err != nil {
		return resp, err
	}
	result := &searchResultResponse{}
	err = json.Unmarshal(data, result)
	if err != nil {
		return resp, err
	}

	items := make([]upstream.RemotePackageName, len(result.Hits))
	for i, hit := range result.Hits {
		items[i] = upstream.RemotePackageName{
			RemoteName: hit.Slug,
			Source:     s.Id(),
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
	remote upstream.RawPackageRemote,
	err error,
) {
	version, err := getVersion(id)
	if err != nil {
		return nil, err
	}
	if len(version.Files) == 0 || path.Ext(version.Files[0].Filename) != ".jar" {
		return nil, ErrUnsupportedFileType
	}
	return version, nil
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

// Support from Modrinth API is extremely unreliable. A local check (if any
// files were downloaded) is recommended.
func (s provider) Support(name types.BarePackageName) (
	supports upstream.RawProjectSupport,
	err error,
) {
	project, err := getProjectByName(name)
	if err != nil {
		return nil, err
	}
	return project, nil
}

var ErrInvalidAPIResponse = errors.New("received non-200 code from modrinth api")

// Temporary guard: Modrinth can ship non-JAR artifacts such as .mrpack,
// but Lucy does not support installing them yet.
var ErrUnsupportedFileType = errors.New("modrinth: only .jar files are supported")

func (s provider) Dependencies(id types.VersionedPackageRef) (
	deps upstream.RawPackageDependencies,
	err error,
) {
	version, err := getVersion(id)
	if err != nil {
		return nil, fmt.Errorf("modrinth: dependencies fetch failed: %w", err)
	}
	return &modrinthDependencies{version: version, platform: id.Platform}, nil
}

func (s provider) ParseAmbiguousId(p types.VersionedPackageRef) (
	parsed types.VersionedPackageRef,
	err error,
) {
	if p.Platform.IsSelector() {
		// Platform inference removed to avoid circular imports.
		// Caller should provide explicit platform.
		p.Platform = types.PlatformNone
	}
	parsed.Platform = p.Platform

	parsed.Name = p.Name

	var v *versionResponse

	switch p.Version {
	case types.VersionCompatible:
		v, err = latestCompatibleVersion(p.Name, p.Platform)
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
