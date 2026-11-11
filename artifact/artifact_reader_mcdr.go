package artifact

import (
	"archive/zip"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/version"
)

type mcdrReader struct{}

var _ Reader = (*mcdrReader)(nil)

func newMcdrReader() Reader {
	return &mcdrReader{}
}

func (r *mcdrReader) Read(
	zipRdr *zip.Reader,
	filePath string,
) ([]PackageInfo, error) {
	decoded, err := decodeMCDRDescriptor(zipRdr, "")
	if err != nil {
		return nil, err
	}
	return packageInfosFromDecoded(filePath, decoded)
}

func decodeMCDRDescriptor(
	zipRdr *zip.Reader,
	member string,
) (*decodedDescriptor, error) {
	raw, err := readZipEntryBytes(zipRdr, "mcdreforged.plugin.json")
	if err != nil || raw == nil {
		return nil, err
	}
	return mcdrDecodedDescriptor(zipRdr, raw, member)
}

type mcdrPluginFile struct {
	ID          string `json:"id"`
	Version     string `json:"version"`
	Name        string `json:"name"`
	Description struct {
		EnUs string `json:"en_us"`
		ZhCn string `json:"zh_cn"`
	} `json:"description"`
	Author           fn.SingleOrSlice[string] `json:"author"`
	Link             string                   `json:"link"`
	Dependencies     map[string]string        `json:"dependencies"`
	Requirements     []string                 `json:"requirements"`
	RequirementsFile jsontext.Value           `json:"requirements_file"`
}

func mcdrDecodedDescriptor(
	zipRdr *zip.Reader,
	raw []byte,
	member string,
) (*decodedDescriptor, error) {
	descriptor := &mcdrPluginFile{}
	if err := json.Unmarshal(raw, descriptor); err != nil {
		return nil, err
	}
	module := catalog.DescriptorModule{
		ID:             descriptor.ID,
		Version:        version.Version(descriptor.Version),
		Ecosystem:      catalog.EcosystemMCDR,
		VersionDialect: version.DialectMCDR,
		NestedPath:     member,
		DescriptorPath: "mcdreforged.plugin.json",
		Dependencies:   []catalog.DescriptorDependency{},
	}
	for id, rangeText := range descriptor.Dependencies {
		module.Dependencies = append(module.Dependencies, catalog.DescriptorDependency{
			ID:         id,
			Kind:       catalog.DependencyKindRequired,
			Constraint: rangeText,
		})
	}
	for _, requirement := range descriptor.Requirements {
		module.Dependencies = append(module.Dependencies, catalog.DescriptorDependency{
			ID:   requirement,
			Kind: catalog.DependencyKindPython,
		})
	}
	if zipRdr != nil {
		files, err := mcdrRequirementFiles(descriptor.RequirementsFile)
		if err != nil {
			return nil, err
		}
		if len(files) == 0 {
			files = []string{"requirements.txt"}
		}
		for _, name := range files {
			content, err := readZipEntryBytes(zipRdr, name)
			if err != nil {
				return nil, err
			}
			if content == nil {
				continue
			}
			for _, requirement := range mcdrRequirements(content) {
				module.Dependencies = append(
					module.Dependencies,
					catalog.DescriptorDependency{
						ID:   requirement,
						Kind: catalog.DependencyKindPython,
					},
				)
			}
		}
	}

	decoded := decodedModule{
		Module:           module,
		PackageEcosystem: catalog.EcosystemMCDR,
		Metadata:         mcdrMetadata(descriptor),
	}
	return &decodedDescriptor{Modules: []decodedModule{decoded}}, nil
}

func mcdrMetadata(descriptor *mcdrPluginFile) catalog.Metadata {
	authors := make([]catalog.Person, len(descriptor.Author))
	for i, author := range descriptor.Author {
		authors[i] = catalog.Person{Name: author}
	}
	urls := make([]catalog.URL, 0, 1)
	if descriptor.Link != "" {
		urls = append(
			urls,
			catalog.URL{
				Name: "Link",
				Type: catalog.URLSource,
				URL:  descriptor.Link,
			},
		)
	}
	return catalog.Metadata{
		Title:       descriptor.Name,
		Description: descriptor.Description.EnUs,
		Authors:     authors,
		URLs:        urls,
	}
}

func mcdrRequirementFiles(raw jsontext.Value) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		if one == "" {
			return nil, nil
		}
		return []string{one}, nil
	}
	var many []string
	if err := json.Unmarshal(raw, &many); err != nil {
		return nil, err
	}
	return many, nil
}

func mcdrRequirements(content []byte) []string {
	var requirements []string
	for line := range strings.SplitSeq(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		requirements = append(requirements, line)
	}
	return requirements
}
