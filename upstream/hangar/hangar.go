package hangar

import (
	"fmt"

	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
)

type provider struct{}

var Provider provider

func (provider) Source() types.Source {
	return types.SourceHangar
}

func (provider) Search(
	query string,
	options types.SearchOptions,
) (res upstream.RawSearchResults, err error) {
	return searchProjects(query, options)
}

func (p provider) Fetch(id types.PackageId) (
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

func (p provider) Metadata(name types.PackageName) (
	info upstream.RawProjectInformation,
	err error,
) {
	return getProject(name)
}

func (p provider) Support(name types.PackageName) (
	supports upstream.RawProjectSupport,
	err error,
) {
	return getProject(name)
}

func (p provider) Dependencies(id types.PackageId) (
	deps upstream.RawPackageDependencies,
	err error,
) {
	version, err := getVersion(id)
	if err != nil {
		return nil, fmt.Errorf("hangar: dependencies fetch failed: %w", err)
	}
	return &hangarDependencies{version: version, platform: id.Platform}, nil
}

func (p provider) ParseAmbiguousId(id types.PackageId) (
	parsed types.PackageId,
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
