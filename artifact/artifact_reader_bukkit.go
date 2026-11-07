package artifact

import (
	"archive/zip"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/version"
	"gopkg.in/yaml.v3"
)

const bukkitPluginDescriptorPath = "plugin.yml"

type bukkitReader struct{}

type bukkitPluginDescriptor struct {
	Name              string   `yaml:"name"`
	Version           string   `yaml:"version"`
	Main              string   `yaml:"main"`
	Description       string   `yaml:"description"`
	Author            string   `yaml:"author"`
	Authors           []string `yaml:"authors"`
	Website           string   `yaml:"website"`
	APIVersion        string   `yaml:"api-version"`
	API               []string `yaml:"api"`
	Depend            []string `yaml:"depend"`
	SoftDepend        []string `yaml:"softdepend"`
	Libraries         []string `yaml:"libraries"`
	FoliaSupported    bool     `yaml:"folia-supported"`
	PaperPluginLoader string   `yaml:"paper-plugin-loader"`
}

func newBukkitReader() Reader { return &bukkitReader{} }

func (r *bukkitReader) Read(
	zipRdr *zip.Reader,
	filePath string,
) ([]PackageInfo, error) {
	for _, f := range zipRdr.File {
		if f.Name != bukkitPluginDescriptorPath {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return nil, err
		}

		descriptor := &bukkitPluginDescriptor{}
		if err := yaml.NewDecoder(rc).Decode(descriptor); err != nil {
			_ = rc.Close()
			return nil, err
		}
		if err := rc.Close(); err != nil {
			return nil, err
		}

		if strings.TrimSpace(descriptor.Name) == "" ||
			strings.TrimSpace(descriptor.Version) == "" ||
			strings.TrimSpace(descriptor.Main) == "" {
			return nil, nil
		}

		platform := detectBukkitPluginPlatform(descriptor)
		info := PackageInfo{
			Ref: catalog.VersionedRef{
				Ref: catalog.Ref{
					Name:     input.ToProjectName(descriptor.Name),
					Provider: catalog.ProviderUnknown,
				},
				Ecosystem: platform,
				Version:   version.Version(strings.TrimSpace(descriptor.Version)),
			},
			FilePath: filePath,
			Compatibility: Compatibility{
				FoliaSupported: descriptor.FoliaSupported,
			},
			Metadata: catalog.Metadata{
				Title:       strings.TrimSpace(descriptor.Name),
				Description: strings.TrimSpace(descriptor.Description),
				Authors: bukkitDescriptorAuthors(
					descriptor.Author,
					descriptor.Authors,
				),
				URLs: bukkitDescriptorURLs(descriptor.Website),
			},
		}

		if deps := bukkitDescriptorDeps(platform, descriptor); len(deps) > 0 {
			info.Dependencies = deps
		}

		return []PackageInfo{info}, nil
	}

	return nil, nil
}

func detectBukkitPluginPlatform(
	descriptor *bukkitPluginDescriptor,
) catalog.Ecosystem {
	if descriptor.PaperPluginLoader != "" || len(descriptor.Libraries) > 0 {
		return catalog.EcosystemPaper
	}
	for _, list := range [][]string{
		descriptor.API,
		descriptor.Depend,
		descriptor.SoftDepend,
	} {
		for _, entry := range list {
			switch strings.ToLower(strings.TrimSpace(entry)) {
			case "paper", "folia", "leaves":
				return catalog.EcosystemPaper
			}
		}
	}
	return catalog.EcosystemBukkit
}

func bukkitDescriptorDeps(
	platform catalog.Ecosystem,
	descriptor *bukkitPluginDescriptor,
) []Dependency {
	deps := make(
		[]Dependency,
		0,
		len(descriptor.Depend)+len(descriptor.SoftDepend),
	)
	deps = appendBukkitDescriptorDeps(deps, platform, descriptor.Depend, true)
	deps = appendBukkitDescriptorDeps(
		deps,
		platform,
		descriptor.SoftDepend,
		false,
	)
	return deps
}

func appendBukkitDescriptorDeps(
	deps []Dependency,
	platform catalog.Ecosystem,
	names []string,
	mandatory bool,
) []Dependency {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		deps = append(
			deps, Dependency{
				Ref: catalog.VersionedRef{
					Ref: catalog.Ref{
						Name:     input.ToProjectName(name),
						Provider: catalog.ProviderUnknown,
					}, Ecosystem: platform,
				},
				Mandatory: mandatory,
			},
		)
	}
	return deps
}

func bukkitDescriptorAuthors(author string, authors []string) []catalog.Person {
	people := make([]catalog.Person, 0, len(authors)+1)
	if trimmed := strings.TrimSpace(author); trimmed != "" {
		people = append(people, catalog.Person{Name: trimmed})
	}
	for _, item := range authors {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			people = append(people, catalog.Person{Name: trimmed})
		}
	}
	return people
}

func bukkitDescriptorURLs(website string) []catalog.URL {
	website = strings.TrimSpace(website)
	if website == "" {
		return nil
	}

	return []catalog.URL{
		{
			Name: "Website",
			Type: catalog.URLHome,
			URL:  website,
		},
	}
}
