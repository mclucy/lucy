package artifact

import (
	"archive/zip"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/version"
	"go.yaml.in/yaml/v4"
)

const (
	bukkitPluginDescriptorPath = "plugin.yml"
	paperPluginDescriptorPath  = "paper-plugin.yml"
)

type bukkitReader struct{}

var _ Reader = (*bukkitReader)(nil)

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
	Provides          []string `yaml:"provides"`
	Depend            []string `yaml:"depend"`
	SoftDepend        []string `yaml:"softdepend"`
	LoadBefore        []string `yaml:"loadbefore"`
	Libraries         []string `yaml:"libraries"`
	FoliaSupported    bool     `yaml:"folia-supported"`
	PaperPluginLoader string   `yaml:"paper-plugin-loader"`
	Dependencies      map[string]map[string]struct {
		Required      *bool  `yaml:"required"`
		Load          string `yaml:"load"`
		JoinClasspath *bool  `yaml:"join-classpath"`
	} `yaml:"dependencies"`
}

func newBukkitReader() Reader { return &bukkitReader{} }

func (r *bukkitReader) Read(
	zipRdr *zip.Reader,
	filePath string,
) ([]PackageInfo, error) {
	decoded, err := decodeBukkitPackageDescriptor(zipRdr, "")
	if err != nil {
		return nil, err
	}
	return packageInfosFromDecoded(filePath, decoded)
}

// decodeBukkitPackageDescriptor reads plugin.yml for the metadata reader view.
func decodeBukkitPackageDescriptor(
	zipRdr *zip.Reader,
	member string,
) (*decodedDescriptor, error) {
	descriptor, name, err := readBukkitPluginDescriptor(
		zipRdr,
		bukkitPluginDescriptorPath,
	)
	if err != nil || descriptor == nil {
		return nil, err
	}
	return bukkitDecodedDescriptor(descriptor, name, member, true)
}

// decodeBukkitDescriptor reads the modules a Bukkit or Paper runtime loads. A
// paper-plugin.yml descriptor takes precedence over plugin.yml, and both
// loaders record the module as Bukkit.
func decodeBukkitDescriptor(
	zipRdr *zip.Reader,
	member string,
) (*decodedDescriptor, error) {
	descriptor, name, err := readBukkitPluginDescriptor(
		zipRdr,
		paperPluginDescriptorPath,
	)
	if err != nil {
		return nil, err
	}
	if descriptor == nil {
		descriptor, name, err = readBukkitPluginDescriptor(
			zipRdr,
			bukkitPluginDescriptorPath,
		)
		if err != nil {
			return nil, err
		}
	}
	if descriptor == nil {
		return nil, nil
	}
	return bukkitDecodedDescriptor(descriptor, name, member, false)
}

func readBukkitPluginDescriptor(
	zipRdr *zip.Reader,
	name string,
) (*bukkitPluginDescriptor, string, error) {
	raw, err := readZipEntryBytes(zipRdr, name)
	if err != nil || raw == nil {
		return nil, name, err
	}
	descriptor := &bukkitPluginDescriptor{}
	if err := yaml.Unmarshal(raw, descriptor); err != nil {
		return nil, name, err
	}
	return descriptor, name, nil
}

func bukkitDecodedDescriptor(
	descriptor *bukkitPluginDescriptor,
	descriptorPath string,
	member string,
	requireMain bool,
) (*decodedDescriptor, error) {
	if requireMain &&
		(strings.TrimSpace(descriptor.Name) == "" ||
			strings.TrimSpace(descriptor.Version) == "" ||
			strings.TrimSpace(descriptor.Main) == "") {
		return nil, nil
	}

	platform := detectBukkitPluginPlatform(descriptor)
	module := catalog.DescriptorModule{
		ID:             descriptor.Name,
		Version:        version.Version(descriptor.Version),
		Ecosystem:      catalog.EcosystemBukkit,
		VersionDialect: version.DialectOpaque,
		NestedPath:     member,
		DescriptorPath: descriptorPath,
		APIVersion:     descriptor.APIVersion,
		FoliaSupported: descriptor.FoliaSupported,
		Provides:       descriptor.Provides,
		Dependencies:   bukkitModuleDependencies(descriptor),
	}
	packageDependencies := bukkitPackageDependencies(descriptor, platform)

	decoded := decodedModule{
		Module:              module,
		PackageEcosystem:    platform,
		PackageVersion:      version.Version(strings.TrimSpace(descriptor.Version)),
		PackageDependencies: packageDependencies,
		Metadata: catalog.Metadata{
			Title:       strings.TrimSpace(descriptor.Name),
			Description: strings.TrimSpace(descriptor.Description),
			Authors: bukkitDescriptorAuthors(
				descriptor.Author,
				descriptor.Authors,
			),
			URLs: bukkitDescriptorURLs(descriptor.Website),
		},
		Compatibility: Compatibility{
			FoliaSupported: descriptor.FoliaSupported,
		},
	}
	return &decodedDescriptor{Modules: []decodedModule{decoded}}, nil
}

func bukkitModuleDependencies(
	descriptor *bukkitPluginDescriptor,
) []catalog.DescriptorDependency {
	dependencies := make(
		[]catalog.DescriptorDependency,
		0,
		len(descriptor.Depend)+len(descriptor.SoftDepend)+
			len(descriptor.LoadBefore),
	)
	dependencies = appendBukkitModuleDependencies(
		dependencies,
		descriptor.Depend,
		catalog.DependencyKindRequired,
		"AFTER",
	)
	dependencies = appendBukkitModuleDependencies(
		dependencies,
		descriptor.SoftDepend,
		catalog.DependencyKindOptional,
		"AFTER",
	)
	dependencies = appendBukkitModuleDependencies(
		dependencies,
		descriptor.LoadBefore,
		catalog.DependencyKindOptional,
		"BEFORE",
	)
	for phase, deps := range descriptor.Dependencies {
		for id, dep := range deps {
			kind := catalog.DependencyKindRequired
			if dep.Required != nil && !*dep.Required {
				kind = catalog.DependencyKindOptional
			}
			join := dep.JoinClasspath == nil || *dep.JoinClasspath
			dependencies = append(
				dependencies,
				catalog.DescriptorDependency{
					ID:            id,
					Kind:          kind,
					Phase:         phase,
					Load:          dep.Load,
					JoinClasspath: join,
				},
			)
		}
	}
	return dependencies
}

func appendBukkitModuleDependencies(
	dependencies []catalog.DescriptorDependency,
	names []string,
	kind catalog.DependencyKind,
	ordering string,
) []catalog.DescriptorDependency {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		dependencies = append(
			dependencies,
			catalog.DescriptorDependency{
				ID:       name,
				Kind:     kind,
				Ordering: ordering,
			},
		)
	}
	return dependencies
}

// bukkitPackageDependencies keeps the reader view to plugin.yml's depend and
// softdepend lists.
func bukkitPackageDependencies(
	descriptor *bukkitPluginDescriptor,
	platform catalog.Ecosystem,
) []Dependency {
	dependencies := make(
		[]Dependency,
		0,
		len(descriptor.Depend)+len(descriptor.SoftDepend),
	)
	dependencies = appendBukkitPackageDependencies(
		dependencies,
		platform,
		descriptor.Depend,
		true,
	)
	dependencies = appendBukkitPackageDependencies(
		dependencies,
		platform,
		descriptor.SoftDepend,
		false,
	)
	return dependencies
}

func appendBukkitPackageDependencies(
	dependencies []Dependency,
	platform catalog.Ecosystem,
	names []string,
	mandatory bool,
) []Dependency {
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		dependencies = append(
			dependencies,
			Dependency{
				Ref: catalog.VersionedRef{
					Ref: catalog.Ref{
						Name:     input.ToProjectName(name),
						Provider: catalog.ProviderUnknown,
					},
					Ecosystem: platform,
				},
				Mandatory: mandatory,
			},
		)
	}
	return dependencies
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
