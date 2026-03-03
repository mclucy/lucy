package detector

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"

	"github.com/mclucy/lucy/exttype"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"

	"gopkg.in/yaml.v3"

	"github.com/mclucy/lucy/logger"
)

const mcdrConfigFileName = "config.yml"

// McdrDetector detects MCDR (MCDReforged) installations
type McdrDetector struct{}

func (d *McdrDetector) Name() string {
	return "mcdr"
}

func (d *McdrDetector) Detect(dir string, env *types.EnvironmentInfo) {
	configPath := path.Join(dir, mcdrConfigFileName)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return
	}

	// File exists, try to read it
	configFile, err := os.Open(configPath)
	if err != nil {
		logger.Warn(err)
		return
	}
	defer func(configFile io.ReadCloser) {
		err := configFile.Close()
		if err != nil {
			logger.Warn(err)
		}
	}(configFile)

	configData, err := io.ReadAll(configFile)
	if err != nil {
		logger.Warn(err)
		return
	}

	config := &exttype.FileMcdrConfig{}
	if err := yaml.Unmarshal(configData, config); err != nil {
		logger.Warn(err)
		return
	}

	bytes, err := exec.Command("mcdreforged", "--version").Output()
	if err != nil {
		logger.ReportWarn(
			fmt.Errorf(
				"cannot execute mcdr, it is in your $PATH?: %w",
				err,
			),
		)
	}

	// `mcdreforged --version` output is like "MCDReforged 2.13.2"
	version := types.RawVersion(strings.Split(string(bytes), " ")[1])
	env.Mcdr = &types.McdrEnv{
		Version: version,
		Config:  config,
	}
	return
}

func init() {
	registerEnvironmentDetector(&McdrDetector{})
	registerOtherPackageDetector(&McdrPluginDetector{})
}

type McdrPluginDetector struct{}

func (d *McdrPluginDetector) Name() string {
	return "mcdr plugin"
}

func (d *McdrPluginDetector) Detect(
	zipReader *zip.Reader,
	fileHandle *os.File,
) (packages []types.Package, err error) {
	var pkg types.Package
	for _, f := range zipReader.File {
		if f.Name == "mcdreforged.plugin.json" {
			r, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer tools.CloseReader(r, logger.Warn)

			data, err := io.ReadAll(r)
			if err != nil {
				return nil, err
			}
			pluginInfo := &exttype.FileMcdrPluginIdentifier{}
			if err := json.Unmarshal(data, pluginInfo); err != nil {
				return nil, err
			}

			pkg = types.Package{
				Id: types.PackageId{
					Platform: types.PlatformMCDR,
					Name:     syntax.ToProjectName(pluginInfo.Id),
					Version:  types.RawVersion(pluginInfo.Version),
				},
				Local: &types.PackageInstallation{
					Path: fileHandle.Name(),
				},
				Dependencies: &types.PackageDependencies{},
				Information:  &types.ProjectInformation{},
			}

			// Parse dependencies
			for key, value := range pluginInfo.Dependencies {
				pkg.Dependencies.Value = append(
					pkg.Dependencies.Value,
					types.Dependency{
						Id: types.PackageId{
							Platform: types.PlatformMCDR,
							Name:     syntax.ToProjectName(key),
						},
						Constraint: parseNpmVersionRange(value),
						Mandatory:  true,
					},
				)
			}

			// Parse info
			pkg.Information.Authors = make(
				[]types.Person,
				len(pluginInfo.Author),
			)
			for i, author := range pluginInfo.Author {
				pkg.Information.Authors[i] = types.Person{
					Name: author,
				}
			}
			pkg.Information.Title = pluginInfo.Name
			pkg.Information.Brief = pluginInfo.Description.EnUs
			pkg.Information.Urls = []types.Url{
				{
					Name: "Link",
					Type: types.UrlSource,
					Url:  pluginInfo.Link,
				},
			}
		}
	}

	packages = append(packages, pkg)
	return packages, nil
}
