package artifact

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"

	"github.com/mclucy/lucy/syntax"
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

func (r *bukkitReader) Read(zipRdr *zip.Reader, filePath string, resolver SlugResolver) ([]ArtifactInfo, error) {
	for _, f := range zipRdr.File {
		if f.Name != bukkitPluginDescriptorPath {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return nil, err
		}

		raw, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, err
		}

		descriptor := &bukkitPluginDescriptor{}
		if err := yaml.NewDecoder(bytes.NewReader(raw)).Decode(descriptor); err != nil {
			return nil, err
		}

		if strings.TrimSpace(descriptor.Name) == "" ||
			strings.TrimSpace(descriptor.Version) == "" ||
			strings.TrimSpace(descriptor.Main) == "" {
			return nil, nil
		}

		platform := detectBukkitPluginPlatform(descriptor)
		info := ArtifactInfo{
			Ref: types.PackageRef{
				Platform: platform,
				Name:     syntax.ToProjectName(descriptor.Name),
			},
			Version:  types.BareVersion(strings.TrimSpace(descriptor.Version)),
			FilePath: filePath,
			Metadata: types.Metadata{
				Title:       strings.TrimSpace(descriptor.Name),
				Description: strings.TrimSpace(descriptor.Description),
				Authors:     bukkitDescriptorAuthors(descriptor.Author, descriptor.Authors),
				Urls:        bukkitDescriptorURLs(descriptor.Website),
			},
			Supports: bukkitDescriptorSupport(platform, descriptor),
		}

		if deps := bukkitDescriptorDeps(platform, descriptor); len(deps) > 0 {
			info.Dependencies = deps
		}

		return []ArtifactInfo{info}, nil
	}

	return nil, nil
}

func detectBukkitPluginPlatform(descriptor *bukkitPluginDescriptor) types.Platform {
	signals := strings.ToLower(strings.Join(append(
		append(
			append([]string{
				descriptor.APIVersion,
				descriptor.PaperPluginLoader,
			}, descriptor.API...),
			descriptor.Depend...,
		),
		append(descriptor.SoftDepend, descriptor.Libraries...)...,
	), " "))

	switch {
	case strings.Contains(signals, "leaves"):
		return types.Platform("leaves")
	case descriptor.FoliaSupported || strings.Contains(signals, "folia"):
		return types.Platform("folia")
	case strings.Contains(signals, "paper") || descriptor.PaperPluginLoader != "" || len(descriptor.Libraries) > 0:
		return types.Platform("paper")
	case strings.Contains(signals, "spigot") || descriptor.APIVersion != "":
		return types.Platform("spigot")
	default:
		return types.PlatformBukkit
	}
}

func bukkitDescriptorDeps(platform types.Platform, descriptor *bukkitPluginDescriptor) []ArtifactDep {
	deps := make([]ArtifactDep, 0, len(descriptor.Depend)+len(descriptor.SoftDepend))
	deps = appendBukkitDescriptorDeps(deps, platform, descriptor.Depend, true)
	deps = appendBukkitDescriptorDeps(deps, platform, descriptor.SoftDepend, false)
	return deps
}

func appendBukkitDescriptorDeps(deps []ArtifactDep, platform types.Platform, names []string, mandatory bool) []ArtifactDep {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		deps = append(deps, ArtifactDep{
			Ref: types.PackageRef{
				Platform: platform,
				Name:     syntax.ToProjectName(name),
			},
			Mandatory: mandatory,
		})
	}
	return deps
}

func bukkitDescriptorSupport(platform types.Platform, descriptor *bukkitPluginDescriptor) *types.PlatformSupport {
	platforms := []types.Platform{platform}
	if platform != types.PlatformBukkit {
		platforms = append(platforms, types.PlatformBukkit)
	}
	if descriptor.FoliaSupported && platform != types.Platform("folia") {
		platforms = append([]types.Platform{types.Platform("folia")}, platforms...)
	}

	return &types.PlatformSupport{
		Platforms: platforms,
		Authentic: true,
	}
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
