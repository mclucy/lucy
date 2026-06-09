package detector

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/syntax"
	"github.com/mclucy/lucy/tools"
	"github.com/mclucy/lucy/types"
	"gopkg.in/yaml.v3"
)

const (
	bukkitPluginDescriptorPath = "plugin.yml"
	paperPluginDescriptorPath  = "paper-plugin.yml"
	leavesPluginDescriptorPath = "leaves-plugin.json"
)

type bukkitDetector struct{}

type bukkitPluginDescriptor struct {
	Name            string   `yaml:"name"`
	Version         string   `yaml:"version"`
	Main            string   `yaml:"main"`
	Description     string   `yaml:"description"`
	Author          string   `yaml:"author"`
	Authors         []string `yaml:"authors"`
	Website         string   `yaml:"website"`
	APIVersion      string   `yaml:"api-version"`
	FoliaSupported  bool     `yaml:"folia-supported"`
	PaperPluginLoad string   `yaml:"paper-plugin-loader"`
}

type paperPluginDescriptor struct {
	Name         string   `yaml:"name"`
	Version      string   `yaml:"version"`
	Main         string   `yaml:"main"`
	Bootstrapper string   `yaml:"bootstrapper"`
	Loader       string   `yaml:"loader"`
	Description  string   `yaml:"description"`
	Author       string   `yaml:"author"`
	Authors      []string `yaml:"authors"`
	Website      string   `yaml:"website"`
	APIVersion   string   `yaml:"api-version"`
}

type leavesPluginDescriptor struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Main         string   `json:"main"`
	Bootstrapper string   `json:"bootstrapper"`
	Loader       string   `json:"loader"`
	Description  string   `json:"description"`
	Author       string   `json:"author"`
	Authors      []string `json:"authors"`
	Website      string   `json:"website"`
	APIVersion   string   `json:"api-version"`
}

func newBukkitDetector() PackageDetector {
	return &bukkitDetector{}
}

func (d *bukkitDetector) Name() string {
	return "paper-family plugin"
}

func (d *bukkitDetector) Detect(
	zipReader *zip.Reader,
	fileHandle *os.File,
) ([]types.Package, error) {
	if data, ok, err := readArchiveEntry(
		zipReader,
		leavesPluginDescriptorPath,
	); err != nil {
		return nil, err
	} else if ok {
		pkg, err := parseLeavesPluginDescriptor(data, fileHandle.Name())
		if err != nil || pkg == nil {
			return nil, err
		}
		return []types.Package{*pkg}, nil
	}

	if data, ok, err := readArchiveEntry(
		zipReader,
		paperPluginDescriptorPath,
	); err != nil {
		return nil, err
	} else if ok {
		pkg, err := parsePaperPluginDescriptor(data, fileHandle.Name())
		if err != nil || pkg == nil {
			return nil, err
		}
		return []types.Package{*pkg}, nil
	}

	if data, ok, err := readArchiveEntry(
		zipReader,
		bukkitPluginDescriptorPath,
	); err != nil {
		return nil, err
	} else if ok {
		pkg, err := parseBukkitPluginDescriptor(data, fileHandle.Name())
		if err != nil || pkg == nil {
			return nil, err
		}
		return []types.Package{*pkg}, nil
	}

	return nil, nil
}

func readArchiveEntry(
	zipReader *zip.Reader,
	name string,
) ([]byte, bool, error) {
	for _, file := range zipReader.File {
		if file.Name != name {
			continue
		}

		r, err := file.Open()
		if err != nil {
			return nil, false, err
		}
		defer tools.CloseReader(r, logger.Warn)

		data, err := io.ReadAll(r)
		if err != nil {
			return nil, false, err
		}

		return data, true, nil
	}

	return nil, false, nil
}

func parseBukkitPluginDescriptor(
	data []byte,
	localPath string,
) (*types.Package, error) {
	var descriptor bukkitPluginDescriptor
	if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&descriptor); err != nil {
		return nil, err
	}

	if strings.TrimSpace(descriptor.Name) == "" ||
		strings.TrimSpace(descriptor.Version) == "" ||
		strings.TrimSpace(descriptor.Main) == "" {
		return nil, nil
	}

	supports := []types.Platform{
		types.Platform("bukkit"),
		types.Platform("spigot"),
		types.Platform("paper"),
	}
	if descriptor.FoliaSupported {
		supports = append(supports, types.Platform("folia"))
	}

	pkg := buildPaperFamilyPackage(
		types.Platform("bukkit"),
		descriptor.Name,
		descriptor.Version,
		localPath,
		descriptor.Description,
		collectDescriptorAuthors(descriptor.Author, descriptor.Authors),
		descriptor.Website,
		supports,
	)

	return &pkg, nil
}

func parsePaperPluginDescriptor(
	data []byte,
	localPath string,
) (*types.Package, error) {
	var descriptor paperPluginDescriptor
	if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&descriptor); err != nil {
		return nil, err
	}

	if strings.TrimSpace(descriptor.Name) == "" ||
		strings.TrimSpace(descriptor.Version) == "" ||
		strings.TrimSpace(descriptor.Main) == "" ||
		strings.TrimSpace(descriptor.APIVersion) == "" {
		return nil, nil
	}

	pkg := buildPaperFamilyPackage(
		types.Platform("paper"),
		descriptor.Name,
		descriptor.Version,
		localPath,
		descriptor.Description,
		collectDescriptorAuthors(descriptor.Author, descriptor.Authors),
		descriptor.Website,
		[]types.Platform{types.Platform("paper")},
	)

	return &pkg, nil
}

func parseLeavesPluginDescriptor(
	data []byte,
	localPath string,
) (*types.Package, error) {
	var descriptor leavesPluginDescriptor
	if err := json.Unmarshal(data, &descriptor); err != nil {
		return nil, err
	}

	if strings.TrimSpace(descriptor.Name) == "" ||
		strings.TrimSpace(descriptor.Version) == "" ||
		strings.TrimSpace(descriptor.Main) == "" {
		return nil, nil
	}

	pkg := buildPaperFamilyPackage(
		types.Platform("leaves"),
		descriptor.Name,
		descriptor.Version,
		localPath,
		descriptor.Description,
		collectDescriptorAuthors(descriptor.Author, descriptor.Authors),
		descriptor.Website,
		[]types.Platform{types.Platform("leaves"), types.Platform("paper")},
	)

	return &pkg, nil
}

func buildPaperFamilyPackage(
	platform types.Platform,
	name string,
	version string,
	localPath string,
	description string,
	authors []types.Person,
	website string,
	supportedPlatforms []types.Platform,
) types.Package {
	return types.Package{
		Id: types.PackageId{
			Platform: platform,
			Name:     syntax.ToProjectName(name),
			Version:  types.BareVersion(strings.TrimSpace(version)),
		},
		Local: &types.PackageInstallation{
			Path: localPath,
		},
		Supports: &types.PlatformSupport{
			Platforms: supportedPlatforms,
			Authentic: true,
		},
		Information: &types.Metadata{
			Title:       strings.TrimSpace(name),
			Description: strings.TrimSpace(description),
			Authors:     authors,
			Urls: buildDescriptorURLs(
				strings.TrimSpace(website),
			),
		},
	}
}

func collectDescriptorAuthors(author string, authors []string) []types.Person {
	people := make([]types.Person, 0, len(authors)+1)
	if trimmed := strings.TrimSpace(author); trimmed != "" {
		people = append(people, types.Person{Name: trimmed})
	}
	for _, item := range authors {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			people = append(people, types.Person{Name: trimmed})
		}
	}
	return people
}

func buildDescriptorURLs(website string) []types.Url {
	if website == "" {
		return nil
	}

	return []types.Url{
		{
			Name: "Website",
			Type: types.UrlHome,
			Url:  website,
		},
	}
}

func init() {
	registerModDetector(newBukkitDetector())
}
