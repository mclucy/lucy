package artifact

import (
	"archive/zip"
	"strings"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/types"
	"gopkg.in/yaml.v3"
)

const bukkitPluginDescriptorPath = "plugin.yml"

type bukkitReader struct{}

var _ = newBukkitReader

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
	resolver SlugResolver,
) ([]Info, error) {
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
		info := Info{
			Ref: types.PackageRef{
				Eco:  platform,
				Name: input.ToProjectName(descriptor.Name),
			},
			Version:  types.BareVersion(strings.TrimSpace(descriptor.Version)),
			FilePath: filePath,
			Metadata: types.Metadata{
				Title:       strings.TrimSpace(descriptor.Name),
				Description: strings.TrimSpace(descriptor.Description),
				Authors: bukkitDescriptorAuthors(
					descriptor.Author,
					descriptor.Authors,
				),
				Urls: bukkitDescriptorURLs(descriptor.Website),
			},
		}

		if deps := bukkitDescriptorDeps(platform, descriptor); len(deps) > 0 {
			info.Dependencies = deps
		}

		return []Info{info}, nil
	}

	return nil, nil
}

func detectBukkitPluginPlatform(descriptor *bukkitPluginDescriptor) types.Ecosystem {
	signals := strings.ToLower(
		strings.Join(
			append(
				append(
					append(
						[]string{
							descriptor.APIVersion,
							descriptor.PaperPluginLoader,
						}, descriptor.API...,
					),
					descriptor.Depend...,
				),
				append(descriptor.SoftDepend, descriptor.Libraries...)...,
			), " ",
		),
	)

	switch {
	case strings.Contains(signals, "leaves"):
		return types.Ecosystem("leaves")
	case descriptor.FoliaSupported || strings.Contains(signals, "folia"):
		return types.Ecosystem("folia")
	case strings.Contains(
		signals,
		"paper",
	) || descriptor.PaperPluginLoader != "" || len(descriptor.Libraries) > 0:
		return types.Ecosystem("paper")
	case strings.Contains(signals, "spigot") || descriptor.APIVersion != "":
		return types.Ecosystem("spigot")
	default:
		return types.EcoBukkit
	}
}

func bukkitDescriptorDeps(
	platform types.Ecosystem,
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
	platform types.Ecosystem,
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
				Ref: types.PackageRef{
					Eco:  platform,
					Name: input.ToProjectName(name),
				},
				Mandatory: mandatory,
			},
		)
	}
	return deps
}

func bukkitDescriptorAuthors(author string, authors []string) []types.Person {
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

func bukkitDescriptorURLs(website string) []types.Url {
	website = strings.TrimSpace(website)
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
