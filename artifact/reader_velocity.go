package artifact

import (
	"archive/zip"
	"encoding/json/v2"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/types"
	"gopkg.in/yaml.v3"
)

type velocityReader struct{}

type bungeeCordReader struct{}

type velocityPluginDescriptor struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Description string   `json:"description"`
	Authors     []string `json:"authors"`
	URL         string   `json:"url"`
}

type bungeeCordPluginDescriptor struct {
	Name        string   `yaml:"name"`
	Version     string   `yaml:"version"`
	Description string   `yaml:"description"`
	Author      string   `yaml:"author"`
	Authors     []string `yaml:"authors"`
	Website     string   `yaml:"website"`
}

func newVelocityReader() Reader {
	return &velocityReader{}
}

func newBungeeCordReader() Reader {
	return &bungeeCordReader{}
}

// Read extracts artifact metadata from velocity-plugin.json inside a Velocity
// plugin JAR.
func (r *velocityReader) Read(
	zipRdr *zip.Reader,
	filePath string,
	resolver SlugResolver,
) ([]Info, error) {
	for _, f := range zipRdr.File {
		if f.Name != "velocity-plugin.json" {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return nil, err
		}

		descriptor := &velocityPluginDescriptor{}
		decodeErr := json.UnmarshalRead(rc, descriptor)
		closeErr := rc.Close()
		if decodeErr != nil {
			return nil, decodeErr
		}
		if closeErr != nil {
			return nil, closeErr
		}

		authors := make([]types.Person, 0, len(descriptor.Authors))
		for _, author := range descriptor.Authors {
			authors = append(authors, types.Person{Name: author})
		}

		urls := make([]types.Url, 0, 1)
		if descriptor.URL != "" {
			urls = append(
				urls, types.Url{
					Name: "Homepage",
					Type: types.UrlHome,
					Url:  descriptor.URL,
				},
			)
		}

		return []Info{
			{
				Ref: types.PackageRef{
					Eco:  types.EcoVelocity,
					Name: input.ToProjectName(descriptor.ID),
				},
				Version:  types.BareVersion(descriptor.Version),
				FilePath: filePath,
				Metadata: types.Metadata{
					Title:       descriptor.Name,
					Description: descriptor.Description,
					Authors:     authors,
					Urls:        urls,
				},
			},
		}, nil
	}

	return nil, nil
}

// Read extracts artifact metadata from bungee.yml inside a BungeeCord plugin
// JAR.
func (r *bungeeCordReader) Read(
	zipRdr *zip.Reader,
	filePath string,
	resolver SlugResolver,
) ([]Info, error) {
	for _, f := range zipRdr.File {
		if f.Name != "bungee.yml" {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return nil, err
		}

		descriptor := &bungeeCordPluginDescriptor{}
		if err := yaml.NewDecoder(rc).Decode(descriptor); err != nil {
			_ = rc.Close()
			return nil, err
		}
		if err := rc.Close(); err != nil {
			return nil, err
		}

		authors := make([]types.Person, 0, len(descriptor.Authors)+1)
		if descriptor.Author != "" {
			authors = append(authors, types.Person{Name: descriptor.Author})
		}
		for _, author := range descriptor.Authors {
			authors = append(authors, types.Person{Name: author})
		}

		urls := make([]types.Url, 0, 1)
		if descriptor.Website != "" {
			urls = append(
				urls, types.Url{
					Name: "Website",
					Type: types.UrlHome,
					Url:  descriptor.Website,
				},
			)
		}

		return []Info{
			{
				Ref: types.PackageRef{
					Eco:  types.EcoBungeecord,
					Name: input.ToProjectName(descriptor.Name),
				},
				Version:  types.BareVersion(descriptor.Version),
				FilePath: filePath,
				Metadata: types.Metadata{
					Title:       descriptor.Name,
					Description: descriptor.Description,
					Authors:     authors,
					Urls:        urls,
				},
			},
		}, nil
	}

	return nil, nil
}
