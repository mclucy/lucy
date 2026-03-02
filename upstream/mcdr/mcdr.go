package mcdr

import (
	"fmt"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

type provider struct{}

func (s provider) Source() types.Source {
	return types.SourceMCDR
}

var Provider provider

// Just a trivial type to implement the SearchResults interface
type mcdrSearchResult []string

func (m mcdrSearchResult) ToSearchResults() types.SearchResults {
	var res types.SearchResults
	for _, id := range m {
		res.Projects = append(res.Projects, syntax.ToProjectName(id))
	}
	res.Source = types.SourceMCDR
	return res
}

// TODO: handle search options

func (s provider) Search(
	query string,
	options types.SearchOptions,
) (res upstream.RawSearchResults, err error) {
	if options.Platform != types.Mcdr && options.Platform != types.AllPlatform {
		return nil, fmt.Errorf(
			"invalid search platform: expected %s, got %s",
			types.Mcdr,
			options.Platform,
		)
	}
	res, err = search(query)
	return
}

func (s provider) Fetch(id types.PackageId) (
	rem upstream.RawPackageRemote,
	err error,
) {
	if id.Version.NeedsInfer() {
		id, err = s.ParseAmbiguousVersion(id)
		if err != nil {
			return nil, err
		}
	}
	rem, err = getRelease(id.Name.Pep8String(), id.Version)
	return
}

func (s provider) Information(name types.ProjectName) (
	info upstream.RawProjectInformation,
	err error,
) {
	plugin, err := getInfo(name.Pep8String())
	if err != nil {
		return nil, err
	}
	meta, err := getMeta(name.Pep8String())
	if err != nil {
		return nil, err
	}
	repo, err := getRepository(name.Pep8String())
	if err != nil {
		return nil, err
	}

	info = rawProjectInformation{
		Info:       plugin,
		Meta:       meta,
		Repository: repo,
	}

	return info, nil
}

func (s provider) Dependencies(id types.PackageId) (
	upstream.RawPackageDependencies,
	error,
) {
	// TODO implement me
	panic("implement me")
}

func (s provider) Support(name types.ProjectName) (
	supports upstream.RawProjectSupport,
	err error,
) {
	// TODO implement me
	panic("implement me")
}

func (s provider) ParseAmbiguousVersion(id types.PackageId) (
	parsed types.PackageId,
	err error,
) {
	var rel *release
	switch id.Version {
	case types.LatestVersion, types.AllVersion:
		rel, err = getLatestRelease(id.Name.Pep8String())
		if err != nil {
			return id, err
		}
	case types.LatestCompatibleVersion:
		_ = probe.ServerInfo()
		panic("implement me")
	default:
		return id, fmt.Errorf(
			"cannot parse version %s for package %s",
			id.Version,
			id.Name,
		)
	}
	parsed = types.PackageId{
		Platform: types.Mcdr,
		Name:     id.Name,
		Version:  types.RawVersion(rel.Meta.Version),
	}
	logger.Debug("parsed from" + id.StringFull() + " to " + parsed.StringFull())
	return parsed, nil
}
