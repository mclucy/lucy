package hangar

import (
	"fmt"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

type provider struct{}

var Provider provider

func (provider) Id() types.SourceId {
	return types.SourceHangar
}

func (p provider) Search(q upstream.Query) (upstream.SearchResponse, error) {
	options := types.SearchOptions{
		IncludeClient:  !q.ExcludeClient,
		SortBy:         q.SortBy,
		FilterPlatform: q.FilterPlatform,
	}
	res, err := searchProjects(q.Keyword, options)
	if err != nil {
		return upstream.SearchResponse{}, err
	}
	return res.ToSearchResults(p.Id()), nil
}

func (p provider) Fetch(id types.VersionedPackageRef) (
	remote upstream.RawPackageRemote,
	err error,
) {
	version, err := getVersion(id)
	if err != nil {
		return nil, err
	}

	preferredPlatform := preferredDownloadPlatform(id.Platform)
	if _, ok := version.ToPackageRemoteForPlatform(preferredPlatform); ok {
		return version, nil
	}
	if remote := version.ToPackageRemote(); remote.FileUrl != "" {
		return version, nil
	}
	return nil, ErrNoDownload
}

func (p provider) Info(ref types.PackageRef) (types.Metadata, error) {
	project, err := getProject(ref.Name)
	if err != nil {
		return types.Metadata{}, err
	}
	info := project.ToProjectInformation()
	info.From = p.Id()
	return info, nil
}

func (p provider) Support(name types.BarePackageName) (
	supports upstream.RawProjectSupport,
	err error,
) {
	return getProject(name)
}

func (p provider) Dependencies(id types.VersionedPackageRef) (
	deps upstream.RawPackageDependencies,
	err error,
) {
	version, err := getVersion(id)
	if err != nil {
		return nil, fmt.Errorf("hangar: dependencies fetch failed: %w", err)
	}
	return &hangarDependencies{version: version, platform: id.Platform}, nil
}

func (p provider) ParseAmbiguousId(id types.VersionedPackageRef) (
	parsed types.VersionedPackageRef,
	err error,
) {
	if id.Platform.IsSelector() {
		id.Platform = types.PlatformNone
	}

	if !id.Version.CanInfer() {
		return id, nil
	}

	version, err := resolveVersion(id)
	if err != nil {
		return id, err
	}

	parsed = id
	parsed.Version = types.BareVersion(version.Name)
	return parsed, nil
}
