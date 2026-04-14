package detector

import (
	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"
)

// parseNpmVersionRange parses MCDR plugin dependency requirements.
//
// References:
//   - https://docs.mcdreforged.com/en/latest/plugin_dev/metadata.html
//   - https://docs.npmjs.com/about-semantic-versioning
//
// Note: call sites remain unchanged in detector; the parser implementation is
// centralized in the dependency package.
func parseNpmVersionRange(s string) types.VersionConstraintExpression {
	return dependency.ParseRange(
		s,
		dependency.InferRangeDialect(types.PlatformMCDR),
		types.Semver,
	)
}

func translateMcdrPlugin(
	pluginInfo *exttype.FileMcdrPluginIdentifier,
	localPath string,
) types.Package {
	pkg := types.Package{
		Id: types.PackageId{
			Platform: types.PlatformMCDR,
			Name:     syntax.ToProjectName(pluginInfo.Id),
			Version:  types.RawVersion(pluginInfo.Version),
		},
		Local: &types.PackageInstallation{
			Path: localPath,
		},
		Dependencies: &types.PackageDependencies{},
		Information:  &types.ProjectInformation{},
	}
	pkg.Dependencies.Value = append(pkg.Dependencies.Value,
		translateMcdrDependencies(pluginInfo.Dependencies)...,
	)
	pkg.Information.Authors = make([]types.Person, len(pluginInfo.Author))
	for i, author := range pluginInfo.Author {
		pkg.Information.Authors[i] = types.Person{Name: author}
	}
	pkg.Information.Title = pluginInfo.Name
	pkg.Information.Brief = pluginInfo.Description.EnUs
	pkg.Information.Urls = []types.Url{{
		Name: "Link",
		Type: types.UrlSource,
		Url:  pluginInfo.Link,
	}}
	return pkg
}

func translateMcdrDependencies(deps map[string]string) []types.Dependency {
	translated := make([]types.Dependency, 0, len(deps))
	for key, value := range deps {
		translated = append(translated, types.Dependency{
			Id: types.PackageId{
				Platform: types.PlatformMCDR,
				Name:     syntax.ToProjectName(key),
			},
			Constraint: parseNpmVersionRange(value),
			Mandatory:  true,
		})
	}
	return translated
}
