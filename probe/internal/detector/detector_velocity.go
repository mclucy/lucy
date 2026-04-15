package detector

import (
	"archive/zip"
	"encoding/json"
	"io"
	"os"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
	"gopkg.in/yaml.v3"
)

type velocityDetector struct{}

type bungeecordDetector struct{}

type velocityPluginDescriptor struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Authors     []string `json:"authors"`
	URL         string   `json:"url"`
}

type bungeecordPluginDescriptor struct {
	Name        string   `yaml:"name"`
	Version     string   `yaml:"version"`
	Description string   `yaml:"description"`
	Author      string   `yaml:"author"`
	Authors     []string `yaml:"authors"`
	Website     string   `yaml:"website"`
}

func newVelocityDetector() PackageDetector {
	return &velocityDetector{}
}

func newBungeecordDetector() PackageDetector {
	return &bungeecordDetector{}
}

func (d *velocityDetector) Name() string {
	return "velocity plugin"
}

func (d *bungeecordDetector) Name() string {
	return "bungeecord plugin"
}

func (d *velocityDetector) Detect(
	zipReader *zip.Reader,
	fileHandle *os.File,
) ([]types.Package, error) {
	for _, f := range zipReader.File {
		if f.Name != "velocity-plugin.json" {
			continue
		}

		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer tools.CloseReader(r, logger.Warn)

		data, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}

		descriptor := &velocityPluginDescriptor{}
		if err := json.Unmarshal(data, descriptor); err != nil {
			return nil, err
		}

		return []types.Package{translateVelocityPlugin(descriptor, fileHandle.Name())}, nil
	}

	return nil, nil
}

func (d *bungeecordDetector) Detect(
	zipReader *zip.Reader,
	fileHandle *os.File,
) ([]types.Package, error) {
	for _, f := range zipReader.File {
		if f.Name != "bungee.yml" {
			continue
		}

		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer tools.CloseReader(r, logger.Warn)

		data, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}

		descriptor := &bungeecordPluginDescriptor{}
		if err := yaml.Unmarshal(data, descriptor); err != nil {
			return nil, err
		}

		return []types.Package{translateBungeecordPlugin(descriptor, fileHandle.Name())}, nil
	}

	return nil, nil
}

func translateVelocityPlugin(
	descriptor *velocityPluginDescriptor,
	localPath string,
) types.Package {
	authors := make([]types.Person, 0, len(descriptor.Authors))
	for _, author := range descriptor.Authors {
		authors = append(authors, types.Person{Name: author})
	}

	urls := make([]types.Url, 0, 1)
	if descriptor.URL != "" {
		urls = append(urls, types.Url{
			Name: "Homepage",
			Type: types.UrlHome,
			Url:  descriptor.URL,
		})
	}

	return types.Package{
		Id: types.PackageId{
			Platform: types.Platform("velocity"),
			Name:     syntax.ToProjectName(descriptor.ID),
			Version:  types.RawVersion(descriptor.Version),
		},
		Local: &types.PackageInstallation{
			Path: localPath,
		},
		Information: &types.ProjectInformation{
			Title:       descriptor.Name,
			Description: descriptor.Description,
			Authors:     authors,
			Urls:        urls,
		},
	}
}

func translateBungeecordPlugin(
	descriptor *bungeecordPluginDescriptor,
	localPath string,
) types.Package {
	authors := make([]types.Person, 0, len(descriptor.Authors)+1)
	if descriptor.Author != "" {
		authors = append(authors, types.Person{Name: descriptor.Author})
	}
	for _, author := range descriptor.Authors {
		authors = append(authors, types.Person{Name: author})
	}

	urls := make([]types.Url, 0, 1)
	if descriptor.Website != "" {
		urls = append(urls, types.Url{
			Name: "Website",
			Type: types.UrlHome,
			Url:  descriptor.Website,
		})
	}

	return types.Package{
		Id: types.PackageId{
			Platform: types.Platform("bungeecord"),
			Name:     syntax.ToProjectName(descriptor.Name),
			Version:  types.RawVersion(descriptor.Version),
		},
		Local: &types.PackageInstallation{
			Path: localPath,
		},
		Information: &types.ProjectInformation{
			Title:       descriptor.Name,
			Description: descriptor.Description,
			Authors:     authors,
			Urls:        urls,
		},
	}
}

func init() {
	registerModDetector(newVelocityDetector())
	registerModDetector(newBungeecordDetector())
}
