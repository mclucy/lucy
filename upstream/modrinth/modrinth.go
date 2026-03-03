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
	"io"
	"net/http"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

type provider struct{}

func (s provider) Source() types.Source {
	return types.SourceModrinth
}

var Provider provider

// Search
//
// For Modrinth search API, see:
// https://docs.modrinth.com/api/operations/searchprojects/
func (s provider) Search(
	query string,
	options types.SearchOptions,
) (res upstream.RawSearchResults, err error) {
	var facets []facetItems
	switch options.FilterPlatform {
	case types.PlatformForge:
		facets = append(facets, facetForgeOnly)
	case types.PlatformFabric:
		facets = append(facets, facetFabricOnly)
	case types.PlatformAny:
		fallthrough
	default:
		facets = append(facets, facetAllLoaders)
	}

	if !options.IncludeClient {
		facets = append(facets, facetServerSupported)
	}

	internalOptions := searchOptions{
		index:  modrinthSearchSortingString(options.SortBy),
		facets: facets,
	}
	searchUrl := searchUrl(types.ProjectName(query), internalOptions)

	// Make the call to Modrinth API
	logger.Debug("searching via modrinth api: " + searchUrl)
	httpRes, err := http.Get(searchUrl)
	defer tools.CloseReader(httpRes.Body, logger.Warn)
	if err != nil {
		return nil, ErrInvalidAPIResponse
	}
	data, err := io.ReadAll(httpRes.Body)
	if err != nil {
		return nil, err
	}
	res = &searchResultResponse{}
	err = json.Unmarshal(data, res)
	if err != nil {
		return nil, err
	}
	return res, nil
}

func (s provider) Fetch(id types.PackageId) (
	remote upstream.RawPackageRemote,
	err error,
) {
	id, err = s.ParseAmbiguousVersion(id)
	version, err := getVersion(id)
	if err != nil {
		return nil, err
	}
	return version, nil
}

func (s provider) Information(name types.ProjectName) (
	info upstream.RawProjectInformation,
	err error,
) {
	project, err := getProjectByName(name)
	if err != nil {
		return nil, err
	}
	return project, nil
}

// Support from Modrinth API is extremely unreliable. A local check (if any
// files were downloaded) is recommended.
func (s provider) Support(name types.ProjectName) (
	supports upstream.RawProjectSupport,
	err error,
) {
	project, err := getProjectByName(name)
	if err != nil {
		return nil, err
	}
	return project, nil
}

var ErrInvalidAPIResponse = errors.New("invalid data from modrinth api")

func (s provider) Dependencies(id types.PackageId) (
	deps upstream.RawPackageDependencies,
	err error,
) {
	// TODO implement me
	panic("implement me")
}

func (s provider) ParseAmbiguousVersion(p types.PackageId) (
	parsed types.PackageId,
	err error,
) {
	parsed.Platform = p.Platform
	parsed.Name = p.Name
	var v *versionResponse

	switch p.Version {
	case types.VersionCompatible:
		v, err = latestCompatibleVersion(p.Name)
	case types.VersionAny, types.VersionNone, types.VersionLatest:
		v, err = latestVersion(p.Name)
	default:
		return p, nil
	}
	if err != nil {
		return p, err
	}
	parsed.Version = types.RawVersion(v.VersionNumber)

	return parsed, nil
}
