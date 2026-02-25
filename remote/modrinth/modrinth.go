// Package modrinth provides functions to interact with Modrinth API
//
// We here use Modrinth terms in private functions:
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

	"github.com/mclucy/lucy/tools"

	"github.com/mclucy/lucy/remote"
	"github.com/mclucy/lucy/types"

	"github.com/mclucy/lucy/logger"
)

type self struct{}

func (s self) Name() types.Source {
	return types.Modrinth
}

var Self self

// Search
//
// For Modrinth search API, see:
// https://docs.modrinth.com/api/operations/searchprojects/
func (s self) Search(
	query string,
	options types.SearchOptions,
) (res remote.RawSearchResults, err error) {
	var facets []facetItems
	switch options.Platform {
	case types.Forge:
		facets = append(facets, facetForge)
	case types.Fabric:
		facets = append(facets, facetFabric)
	case types.AllPlatform:
		fallthrough
	default:
		facets = append(facets, facetForge, facetAllLoaders)
	}

	if options.ShowClientPackage {
		facets = append(facets, facetServerSupported, facetClientSupported)
	} else {
		facets = append(facets, facetServerSupported)
	}

	internalOptions := searchOptions{
		index:  options.IndexBy.ToModrinth(),
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

func (s self) Fetch(id types.PackageId) (
	remote remote.RawPackageRemote,
	err error,
) {
	id, err = s.ParseAmbiguousVersion(id)
	version, err := getVersion(id)
	if err != nil {
		return nil, err
	}
	return version, nil
}

func (s self) Information(name types.ProjectName) (
	info remote.RawProjectInformation,
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
func (s self) Support(name types.ProjectName) (
	supports remote.RawProjectSupport,
	err error,
) {
	project, err := getProjectByName(name)
	if err != nil {
		return nil, err
	}
	return project, nil
}

var ErrInvalidAPIResponse = errors.New("invalid data from modrinth api")

func (s self) Dependencies(id types.PackageId) (
	deps remote.RawPackageDependencies,
	err error,
) {
	// TODO implement me
	panic("implement me")
}

func (s self) ParseAmbiguousVersion(p types.PackageId) (
	parsed types.PackageId,
	err error,
) {
	parsed.Platform = p.Platform
	parsed.Name = p.Name
	var v *versionResponse

	switch p.Version {
	case types.LatestCompatibleVersion:
		v, err = LatestCompatibleVersion(p.Name)
	case types.AllVersion, types.NoVersion, types.LatestVersion:
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
