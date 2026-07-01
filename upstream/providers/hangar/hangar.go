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
	options := upstream.SearchOptions{
		IncludeClient:   !q.ExcludeClient,
		FilterEcosystem: q.FilterEcosystem,
	}
	res, err := searchProjects(q.Keyword, options)
	if err != nil {
		return upstream.SearchResponse{}, err
	}
	return res.ToSearchResults(p.Id()), nil
}

func (p provider) Fetch(id types.VersionedPackageRef) (
	types.ResolvedPackage,
	error,
) {
	version, err := getVersion(id)
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	preferredPlatform := preferredDownloadPlatform(id.Eco)
	if remote, ok := version.ToPackageRemoteForPlatform(preferredPlatform); ok {
		return types.ResolvedPackage{
			FileUrl:       remote.FileUrl,
			Filename:      remote.Filename,
			Hash:          remote.Hash,
			HashAlgorithm: remote.HashAlgorithm,
		}, nil
	}
	if remote := version.ToPackageRemote(); remote.FileUrl != "" {
		return types.ResolvedPackage{
			FileUrl:       remote.FileUrl,
			Filename:      remote.Filename,
			Hash:          remote.Hash,
			HashAlgorithm: remote.HashAlgorithm,
		}, nil
	}
	return types.ResolvedPackage{}, ErrNoDownload
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

func (p provider) Dependencies(
	id types.VersionedPackageRef,
) (*types.PackageDependencies, error) {
	version, err := getVersion(id)
	if err != nil {
		return nil, fmt.Errorf("hangar: dependencies fetch failed: %w", err)
	}
	return new(
		(&hangarDependencies{
			version:  version,
			platform: id.Eco,
		}).ToPackageDependencies(),
	), nil
}

func (p provider) ResolveVersionSelector(id types.VersionedPackageRef) (
	parsed types.VersionedPackageRef,
	err error,
) {
	if id.Eco.IsSelector() {
		id.Eco = types.EcoUnspecified
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
