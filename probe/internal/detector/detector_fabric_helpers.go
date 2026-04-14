package detector

import (
	"github.com/mclucy/lucy/dependency"
	externaltype "github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
	"strings"
)

// parseFabricVersionRanges parses a Fabric VersionRange value where each item
// in the outer slice is an OR alternative.
func parseFabricVersionRanges(
	ranges tools.SingleOrSlice[string],
) types.VersionConstraintExpression {
	return dependency.ParseRanges(
		[]string(ranges),
		dependency.InferRangeDialect(types.PlatformFabric),
		types.Semver,
	)
}

func translateFabricMod(
	modInfo *externaltype.FileFabricModIdentifier,
	localPath string,
) types.Package {
	pkg := types.Package{
		Id: types.PackageId{
			Platform: types.PlatformFabric,
			Name:     syntax.ToProjectName(modInfo.Id),
			Version:  types.RawVersion(modInfo.Version),
		},
		Local: &types.PackageInstallation{
			Path: localPath,
		},
		Dependencies: &types.PackageDependencies{},
		Information:  &types.ProjectInformation{},
	}

	embeddedNames := fabricEmbeddedModNames(modInfo)
	pkg.Dependencies.Value = append(pkg.Dependencies.Value,
		translateFabricDependencyMap(modInfo.Depends, true, false, embeddedNames)...,
	)
	pkg.Dependencies.Value = append(pkg.Dependencies.Value,
		translateFabricDependencyMap(modInfo.Recommends, false, false, embeddedNames)...,
	)
	pkg.Dependencies.Value = append(pkg.Dependencies.Value,
		translateFabricDependencyMap(modInfo.Suggests, false, false, embeddedNames)...,
	)
	pkg.Dependencies.Value = append(pkg.Dependencies.Value,
		translateFabricDependencyMap(modInfo.Breaks, false, true, embeddedNames)...,
	)
	pkg.Dependencies.Value = append(pkg.Dependencies.Value,
		translateFabricDependencyMap(modInfo.Conflicts, false, true, embeddedNames)...,
	)

	pkg.Information = &types.ProjectInformation{
		Title:       modInfo.Name,
		Description: modInfo.Description,
		License:     modInfo.License,
		Authors: func() []types.Person {
			authors := make([]types.Person, len(modInfo.Authors))
			for i, author := range modInfo.Authors {
				authors[i] = types.Person{Name: string(author)}
			}
			return authors
		}(),
	}

	return pkg
}

func translateFabricDependencyMap(
	deps map[string]tools.SingleOrSlice[string],
	mandatory bool,
	inverse bool,
	embeddedNames map[string]struct{},
) []types.Dependency {
	translated := make([]types.Dependency, 0, len(deps))
	for k, v := range deps {
		name := syntax.ToProjectName(k)
		_, embedded := embeddedNames[string(name)]
		dep := types.Dependency{
			Id: types.PackageId{
				Platform: types.PlatformFabric,
				Name:     name,
			},
			Constraint: parseFabricVersionRanges(v),
			Mandatory:  mandatory,
			Embedded:   embedded,
		}
		if inverse {
			dep.Constraint.Inverse()
		}
		translated = append(translated, dep)
	}
	return translated
}

func fabricEmbeddedModNames(modInfo *externaltype.FileFabricModIdentifier) map[string]struct{} {
	depNames := make([]string, 0, len(modInfo.Depends))
	for k := range modInfo.Depends {
		depNames = append(depNames, k)
	}

	names := make(map[string]struct{}, len(modInfo.Jars))
	for _, jar := range modInfo.Jars {
		base := jar.File
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			base = base[idx+1:]
		}
		base = strings.TrimSuffix(base, ".jar")
		for _, dep := range depNames {
			if base == dep || strings.HasPrefix(base, dep+"-") {
				names[dep] = struct{}{}
				break
			}
		}
	}
	return names
}
