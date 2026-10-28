package hangar

import (
	"context"
	"fmt"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/upstream"
	versionpkg "github.com/mclucy/lucy/version"
)

type provider struct{}

var Provider provider

func (provider) Provider() catalog.Provider {
	return catalog.ProviderHangar
}

// Builds lists every published version. Hangar versions declare supported
// platforms independently, so each platform becomes its own build entry.
func (p provider) Builds(ctx context.Context, project string) ([]upstream.Build, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	versions, err := listVersions(catalog.Name(project))
	if err != nil {
		return nil, err
	}

	var builds []upstream.Build
	for i := range versions {
		builds = append(builds, versions[i].ToBuilds()...)
	}
	return builds, nil
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
	id catalog.VersionedRef,
) (upstream.ResolvedPackage, error) {
	version, err := getVersion(id)
	if err != nil {
		return upstream.ResolvedPackage{}, err
	}

	preferredPlatform := preferredDownloadPlatform(id.Ecosystem)
	if remote, ok := version.ToPackageRemoteForPlatform(preferredPlatform); ok {
		return upstream.ResolvedPackage{
			FileURL:       remote.FileURL,
			Filename:      remote.Filename,
			Hash:          remote.Hash,
			HashAlgorithm: remote.HashAlgorithm,
		}, nil
	}
	if remote := version.ToPackageRemote(); remote.FileURL != "" {
		return upstream.ResolvedPackage{
			FileURL:       remote.FileURL,
			Filename:      remote.Filename,
			Hash:          remote.Hash,
			HashAlgorithm: remote.HashAlgorithm,
		}, nil
	}
	return upstream.ResolvedPackage{}, ErrNoDownload
}

func (p provider) Info(ref catalog.Ref) (catalog.Metadata, error) {
	project, err := getProject(ref.Name)
	if err != nil {
		return catalog.Metadata{}, err
	}
	info := project.ToProjectInformation()
	return info, nil
}

func (p provider) Dependencies(
	_ upstream.LocalContext,
	id catalog.VersionedRef,
) (*upstream.DependencySet, error) {
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
	id catalog.VersionedRef,
) (parsed catalog.VersionedRef, err error) {
	if id.Ecosystem.IsUnspecified() {
		id.Ecosystem = catalog.EcosystemUnspecified
	}

	if !id.Version.CanInfer() {
		return id, nil
	}

	version, err := resolveVersion(id)
	if err != nil {
		return id, err
	}

	parsed = id
	parsed.Version = versionpkg.Version(version.Name)
	return parsed, nil
}
