package mcdr

import (
	"fmt"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/probe"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

type provider struct{}

func (s provider) Id() types.SourceId {
	return types.SourceMCDR
}

var Provider provider

// Just a trivial type to implement the search response conversion.
type mcdrSearchResult []string

func (m mcdrSearchResult) ToSearchResults(source types.SourceId) upstream.SearchResponse {
	res := upstream.SearchResponse{Source: source}
	for _, id := range m {
		res.Items = append(
			res.Items, upstream.RemotePackageName{
				RemoteName: input.ToProjectName(id).String(),
				Source:     source,
			},
		)
	}
	return res
}

// TODO: handle search options

func (s provider) Search(q upstream.Query) (upstream.SearchResponse, error) {
	if q.FilterPlatform != types.PlatformMCDR && q.FilterPlatform != types.PlatformAny {
		return upstream.SearchResponse{}, fmt.Errorf(
			"invalid search platform: expected %s, got %s",
			types.PlatformMCDR,
			q.FilterPlatform,
		)
	}
	res, err := search(q.Keyword)
	if err != nil {
		return upstream.SearchResponse{}, err
	}
	return res.ToSearchResults(s.Id()), nil
}

func (s provider) Fetch(id types.VersionedPackageRef) (
	rem upstream.RawPackageRemote,
	err error,
) {
	rem, err = getRelease(id.Name.Pep8String(), id.Version)
	return
}

func (s provider) Info(ref types.PackageRef) (types.Metadata, error) {
	name := ref.Name.Pep8String()
	plugin, err := getInfo(name)
	if err != nil {
		return types.Metadata{}, err
	}
	meta, err := getMeta(name)
	if err != nil {
		return types.Metadata{}, err
	}
	repo, err := getRepository(name)
	if err != nil {
		return types.Metadata{}, err
	}

	info := rawProjectInformation{
		Info:       plugin,
		Meta:       meta,
		Repository: repo,
	}.ToProjectInformation()
	info.From = s.Id()
	return info, nil
}

func (s provider) Dependencies(id types.VersionedPackageRef) (
	upstream.RawPackageDependencies,
	error,
) {
	// TODO implement me
	panic("implement me")
}

func (s provider) Support(name types.BarePackageName) (
	supports upstream.RawProjectSupport,
	err error,
) {
	// TODO implement me
	panic("implement me")
}

func (s provider) ResolveVersionSelector(id types.VersionedPackageRef) (
	parsed types.VersionedPackageRef,
	err error,
) {
	var rel *release
	switch id.Version {
	case types.VersionCompatible:
		serverInfo := probe.ServerInfo()
		rel, err = getLatestCompatibleRelease(
			id.Name.Pep8String(),
			serverInfo.Environments.Mcdr.Version,
		)
	case types.VersionLatest, types.VersionAny:
		rel, err = getLatestRelease(id.Name.Pep8String())
		if err != nil {
			return id, err
		}
	default:
		return id, fmt.Errorf(
			"cannot parse version %s for package %s",
			id.Version,
			id.Name,
		)
	}
	if err != nil {
		return id, err
	}
	parsed = types.VersionedPackageRef{
		PackageRef: types.PackageRef{
			Platform: types.PlatformMCDR,
			Name:     id.Name,
		},
		Version: types.BareVersion(rel.Meta.Version),
	}
	logger.Debug("parsed from" + id.StringFull() + " to " + parsed.StringFull())
	return parsed, nil
}
