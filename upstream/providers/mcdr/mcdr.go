package mcdr

import (
	"fmt"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/log"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/upstream"
	"github.com/mclucy/lucy/version"
)

type provider struct{}

func (s provider) Provider() types.Provider {
	return types.ProviderMCDR
}

var Provider provider

type mcdrSearchResult []string

func (m mcdrSearchResult) ToSearchResults(source types.Provider) upstream.SearchResponse {
	res := upstream.SearchResponse{Provider: source}
	for _, id := range m {
		res.Items = append(
			res.Items, upstream.SearchResult{
				ProjectName: input.ToProjectName(id).String(),
				Provider:    source,
			},
		)
	}
	return res
}

// Search returns the provider's default result set; tags and limit are not
// applied.

func (s provider) Search(q upstream.Query) (upstream.SearchResponse, error) {
	// MCDR publishes server plugins only; IncludeClient needs no filter.
	if q.FilterEcosystem != types.EcosystemMCDR && q.FilterEcosystem != types.EcosystemUnspecified {
		return upstream.SearchResponse{}, fmt.Errorf(
			"invalid search platform: expected %s, got %s",
			types.EcosystemMCDR,
			q.FilterEcosystem,
		)
	}
	res, err := search(q.Keyword)
	if err != nil {
		return upstream.SearchResponse{}, err
	}
	return res.ToSearchResults(s.Provider()), nil
}

func (s provider) Fetch(
	_ upstream.LocalContext,
	id types.VersionedPackageRef,
) (types.ResolvedPackage, error) {
	rel, err := getRelease(id.Name.PythonName(), id.Version)
	if err != nil {
		return types.ResolvedPackage{}, err
	}
	r := rel.ToPackageRemote()
	return types.ResolvedPackage{
		FileURL:       r.FileURL,
		Filename:      r.Filename,
		Hash:          r.Hash,
		HashAlgorithm: r.HashAlgorithm,
	}, nil
}

func (s provider) Info(ref types.PackageRef) (types.Metadata, error) {
	name := ref.Name.PythonName()
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
	return info, nil
}

func (s provider) Dependencies(
	local upstream.LocalContext,
	id types.VersionedPackageRef,
) (*types.DependencySet, error) {
	if id.Version == "" || id.Version.CanInfer() {
		resolved, err := s.ResolveVersionSelector(local, id)
		if err != nil {
			return nil, err
		}
		id = resolved
	}

	rel, err := getRelease(id.Name.PythonName(), id.Version)
	if err != nil {
		return nil, err
	}
	dependencies, err := mcdrDependenciesFromMeta(rel.Meta)
	if err != nil {
		return nil, err
	}
	return new(dependencies), nil
}

func mcdrDependenciesFromMeta(meta pluginMeta) (types.DependencySet, error) {
	deps := types.DependencySet{FromArtifact: true}
	if len(meta.Dependencies) == 0 {
		return deps, nil
	}

	deps.Dependencies = make([]types.Dependency, 0, len(meta.Dependencies))
	for name, constraint := range meta.Dependencies {
		parsed, err := version.ParseConstraint(
			constraint,
			types.VersionDialectMCDR,
		)
		if err != nil {
			return types.DependencySet{}, err
		}
		deps.Dependencies = append(
			deps.Dependencies, types.Dependency{
				ID: types.VersionedPackageRef{
					PackageRef: types.PackageRef{
						Name:     input.ToProjectName(name),
						Provider: types.ProviderMCDR,
					},
					Ecosystem: types.EcosystemMCDR,
				},
				Constraint: parsed,
				Mandatory:  true,
			},
		)
	}
	return deps, nil
}

func (s provider) ResolveVersionSelector(
	local upstream.LocalContext,
	id types.VersionedPackageRef,
) (parsed types.VersionedPackageRef, err error) {
	var rel *release
	switch id.Version {
	case types.VersionStable:
		rel, err = getLatestCompatibleRelease(
			id.Name.PythonName(),
			local.MCDRVersion,
		)
	case types.VersionBeta, types.VersionAny:
		rel, err = getLatestRelease(id.Name.PythonName())
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
		Ecosystem: types.EcosystemMCDR,
		Name:      id.Name,
		Version:   types.Version(rel.Meta.Version),
	}
	log.Debug("parsed from" + id.StringQualified() + " to " + parsed.StringQualified())
	return parsed, nil
}
