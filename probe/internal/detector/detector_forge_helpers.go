package detector

import (
	"path/filepath"
	"regexp"
	"strings"

	"github.com/mclucy/lucy/dependency"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/types"
)

type modLoaderDependencySpec struct {
	modID        string
	mandatory    bool
	versionRange string
}

var forgeRuntimeVersionDirPattern = regexp.MustCompile(
	`^(\d+\.\d+(?:\.\d+)?)-(\d+(?:\.\d+)+)$`,
)

var forgeJarNameVersionPattern = regexp.MustCompile(
	`^forge-(\d+\.\d+(?:\.\d+)?)-(\d+(?:\.\d+)+)(?:-[a-z]+)?\.jar$`,
)

// parseModLoaderMavenVersionRange parses Forge dependency version ranges.
//
// References:
//   - https://docs.minecraftforge.net/en/latest/gettingstarted/modfiles/
//   - https://maven.apache.org/enforcer/enforcer-rules/versionRanges.html
func parseModLoaderMavenVersionRange(interval string) [][]types.VersionConstraint {
	return dependency.ParseRange(
		interval,
		dependency.InferRangeDialect(types.PlatformForge),
		types.Semver,
	)
}

func translateModLoaderPackage(
	platform types.Platform,
	localPath string,
	modID string,
	version types.RawVersion,
	deps []modLoaderDependencySpec,
	license string,
	displayName string,
	description string,
	authors string,
	displayURL string,
	issueTrackerURL string,
) types.Package {
	pkg := types.Package{
		Id: types.PackageId{
			Platform: platform,
			Name:     syntax.ToProjectName(modID),
			Version:  version,
		},
		Local: &types.PackageInstallation{
			Path: localPath,
		},
		Dependencies: &types.PackageDependencies{},
		Information:  &types.ProjectInformation{},
	}
	pkg.Dependencies.Value = append(pkg.Dependencies.Value,
		translateModLoaderDependencies(platform, deps)...,
	)
	pkg.Information = &types.ProjectInformation{
		Title:   displayName,
		Brief:   description,
		Authors: []types.Person{{Name: authors}},
		License: license,
		Urls: []types.Url{
			{
				Name: "URL",
				Type: types.UrlHome,
				Url:  displayURL,
			},
			{
				Name: "Issue Tracker",
				Type: types.UrlIssues,
				Url:  issueTrackerURL,
			},
		},
	}
	return pkg
}

func translateModLoaderDependencies(
	platform types.Platform,
	deps []modLoaderDependencySpec,
) []types.Dependency {
	translated := make([]types.Dependency, 0, len(deps))
	for _, dep := range deps {
		translated = append(translated, types.Dependency{
			Id: types.PackageId{
				Platform: platform,
				Name:     syntax.ToProjectName(dep.modID),
			},
			Constraint: parseModLoaderMavenVersionRange(dep.versionRange),
			Mandatory:  dep.mandatory,
		})
	}
	return translated
}

func parseForgeVersionTupleFromPath(
	filePath string,
) (gameVersion types.RawVersion, forgeVersion types.RawVersion, ok bool) {
	parts := strings.Split(filepath.ToSlash(filePath), "/")
	for i := 0; i < len(parts)-1; i++ {
		if parts[i] != "forge" {
			continue
		}
		match := forgeRuntimeVersionDirPattern.FindStringSubmatch(parts[i+1])
		if match == nil {
			continue
		}
		return types.RawVersion(match[1]), types.RawVersion(match[2]), true
	}
	if match := forgeJarNameVersionPattern.FindStringSubmatch(filepath.Base(filePath)); match != nil {
		return types.RawVersion(match[1]), types.RawVersion(match[2]), true
	}
	return types.VersionUnknown, types.VersionUnknown, false
}

func hasConcreteVersion(version types.RawVersion) bool {
	return version != "" && !version.IsInvalid() && !version.CanInfer()
}

func buildForgeRuntimeInfo(
	filePath string,
	gameVersion types.RawVersion,
	forgeVersion types.RawVersion,
) *types.RuntimeInfo {
	return &types.RuntimeInfo{
		PrimaryEntrance: filePath,
		GameVersion:     gameVersion,
		BootCommand:     nil,
		RuntimeIdentities: []types.PackageId{
			{
				Platform: types.PlatformForge,
				Name:     "forge",
				Version:  forgeVersion,
			},
			{
				Platform: types.PlatformMinecraft,
				Name:     "minecraft",
				Version:  gameVersion,
			},
		},
		Topology: &types.RuntimeTopology{
			PrimaryNode: "forge",
			Nodes: []types.RuntimeNode{
				{
					ID:               "forge",
					Role:             types.RuntimeRoleModLoader,
					IdentityPlatform: types.PlatformForge,
					Capabilities:     []types.RuntimeCapability{types.CapabilityForgeMods},
				},
			},
		},
	}
}
