package hangar

import (
	"fmt"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

type provider struct{}

var Provider provider

func (provider) Provider() types.Provider {
	return types.ProviderHangar
}

func (p provider) Search(q upstream.Query) (upstream.SearchResponse, error) {
	// Hangar publishes server plugins only; IncludeClient needs no filter.
	res, err := searchProjects(q.Keyword, q.FilterEcosystem)
	if err != nil {
		return upstream.SearchResponse{}, err
	}
	return res.ToSearchResults(p.Provider()), nil
}

func (p provider) Fetch(
	_ upstream.LocalContext,
	id types.VersionedPackageRef,
) (types.ResolvedPackage, error) {
	version, err := getVersion(id)
	if err != nil {
		return types.ResolvedPackage{}, err
	}

	preferredPlatform := preferredDownloadPlatform(id.Ecosystem)
	if remote, ok := version.ToPackageRemoteForPlatform(preferredPlatform); ok {
		return types.ResolvedPackage{
			FileURL:       remote.FileURL,
			Filename:      remote.Filename,
			Hash:          remote.Hash,
			HashAlgorithm: remote.HashAlgorithm,
		}, nil
	}
	if remote := version.ToPackageRemote(); remote.FileURL != "" {
		return types.ResolvedPackage{
			FileURL:       remote.FileURL,
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
	return info, nil
}

func (p provider) Dependencies(
	_ upstream.LocalContext,
	id types.VersionedPackageRef,
) (*types.DependencySet, error) {
	version, err := getVersion(id)
	if err != nil {
		return nil, fmt.Errorf("hangar: dependencies fetch failed: %w", err)
	}
	return new(
		(&hangarDependencies{
			version:  version,
			platform: id.Ecosystem,
		}).ToDependencySet(),
	), nil
}

func (p provider) ResolveVersionSelector(
	_ upstream.LocalContext,
	id types.VersionedPackageRef,
) (parsed types.VersionedPackageRef, err error) {
	if id.Ecosystem.IsUnspecified() {
		id.Ecosystem = types.EcosystemUnspecified
	}

	if !id.Version.CanInfer() {
		return id, nil
	}

	version, err := resolveVersion(id)
	if err != nil {
		return id, err
	}

	parsed = id
	parsed.Version = types.Version(version.Name)
	return parsed, nil
}
