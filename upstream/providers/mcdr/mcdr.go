package mcdr

import (
	"fmt"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
	"github.com/mclucy/lucy/workspace"
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
			res.Items, upstream.SearchResult{
				RemoteName: input.ToProjectName(id).String(),
				Source:     source,
			},
		)
	}
	return res
}

// TODO: handle search options

func (s provider) Search(q upstream.Query) (upstream.SearchResponse, error) {
	if q.FilterEcosystem != types.EcoMcdr && q.FilterEcosystem != types.EcoUnspecified {
		return upstream.SearchResponse{}, fmt.Errorf(
			"invalid search platform: expected %s, got %s",
			types.EcoMcdr,
			q.FilterEcosystem,
		)
	}
	res, err := search(q.Keyword)
	if err != nil {
		return upstream.SearchResponse{}, err
	}
	return res.ToSearchResults(s.Id()), nil
}

func (s provider) Fetch(id types.VersionedPackageRef) (
	types.ResolvedPackage,
	error,
) {
	rel, err := getRelease(id.Name.Pep8String(), id.Version)
	if err != nil {
		return types.ResolvedPackage{}, err
	}
	r := rel.ToPackageRemote()
	return types.ResolvedPackage{
		FileUrl:       r.FileUrl,
		Filename:      r.Filename,
		Hash:          r.Hash,
		HashAlgorithm: r.HashAlgorithm,
	}, nil
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

func (s provider) Dependencies(
	id types.VersionedPackageRef,
) (*types.PackageDependencies, error) {
	if id.Version == "" || id.Version.CanInfer() {
		resolved, err := s.ResolveVersionSelector(id)
		if err != nil {
			return nil, err
		}
		id = resolved
	}

	rel, err := getRelease(id.Name.Pep8String(), id.Version)
	if err != nil {
		return nil, err
	}
	return new(mcdrDependenciesFromMeta(rel.Meta)), nil
}

func mcdrDependenciesFromMeta(meta pluginMeta) types.PackageDependencies {
	deps := types.PackageDependencies{Authentic: true}
	if len(meta.Dependencies) == 0 {
		return deps
	}

	deps.Value = make([]types.Dependency, 0, len(meta.Dependencies))
	for name, constraint := range meta.Dependencies {
		deps.Value = append(
			deps.Value, types.Dependency{
				Id: types.VersionedPackageRef{
					PackageRef: types.PackageRef{
						Eco:  types.EcoMcdr,
						Name: input.ToProjectName(name),
					},
				},
				Constraint: version.ParseRange(
					constraint,
					version.InferRangeDialect(types.EcoMcdr),
					types.Semver,
				),
				Mandatory: true,
			},
		)
	}
	return deps
}

func (s provider) ResolveVersionSelector(id types.VersionedPackageRef) (
	parsed types.VersionedPackageRef,
	err error,
) {
	var rel *release
	switch id.Version {
	case types.VersionCompatible:
		ws := workspace.New()
		rel, err = getLatestCompatibleRelease(
			id.Name.Pep8String(),
			ws.Environments.Mcdr.Version,
		)
	case "", types.VersionLatest, types.VersionAny:
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
			Eco:  types.EcoMcdr,
			Name: id.Name,
		},
		Version: types.BareVersion(rel.Meta.Version),
	}
	log.Debug("parsed from" + id.StringFull() + " to " + parsed.StringFull())
	return parsed, nil
}
