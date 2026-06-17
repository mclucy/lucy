package artifact

import (
	"archive/zip"
	"encoding/json"
	"io"
	"strings"

	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/logger"
	"github.com/mclucy/lucy/types"
	"github.com/mclucy/lucy/version"
)

const spongePluginMetadataPath = "META-INF/sponge_plugins.json"

type spongeReader struct{}

func newSpongeReader() Reader {
	return &spongeReader{}
}

func (r *spongeReader) Read(
	zipRdr *zip.Reader,
	filePath string,
	resolver SlugResolver,
) ([]ArtifactInfo, error) {
	for _, f := range zipRdr.File {
		if f.Name != spongePluginMetadataPath {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		fn.CloseReader(rc, logger.Warn)

		data, err := io.ReadAll(rc)
		if err != nil {
			return nil, err
		}

		var metadata fileschema.FileSpongePluginsIdentifier
		if err := json.Unmarshal(data, &metadata); err != nil {
			return nil, err
		}

		if !validSpongeMetadata(&metadata) {
			return []ArtifactInfo{}, nil
		}

		infos := make([]ArtifactInfo, 0, len(metadata.Plugins))
		for _, plugin := range metadata.Plugins {
			info, ok := translateSpongePlugin(&metadata, plugin, filePath)
			if !ok {
				continue
			}
			infos = append(infos, info)
		}

		if len(infos) == 0 {
			return []ArtifactInfo{}, nil
		}
		return infos, nil
	}

	return nil, nil
}

func validSpongeMetadata(metadata *fileschema.FileSpongePluginsIdentifier) bool {
	if strings.TrimSpace(metadata.Loader.Name) == "" ||
		strings.TrimSpace(metadata.Loader.Version) == "" ||
		len(metadata.Plugins) == 0 {
		return false
	}

	for _, plugin := range metadata.Plugins {
		if hasConcreteSpongePluginIdentity(metadata, plugin) {
			return true
		}
	}

	return false
}

func hasConcreteSpongePluginIdentity(
	metadata *fileschema.FileSpongePluginsIdentifier,
	plugin fileschema.FileSpongePluginMetadata,
) bool {
	if strings.TrimSpace(plugin.ID) == "" || strings.TrimSpace(plugin.Entrypoint) == "" {
		return false
	}
	return strings.TrimSpace(resolveSpongePluginVersion(metadata, plugin)) != ""
}

func translateSpongePlugin(
	metadata *fileschema.FileSpongePluginsIdentifier,
	plugin fileschema.FileSpongePluginMetadata,
	localPath string,
) (ArtifactInfo, bool) {
	if !hasConcreteSpongePluginIdentity(metadata, plugin) {
		return ArtifactInfo{}, false
	}

	v := resolveSpongePluginVersion(metadata, plugin)
	info := ArtifactInfo{
		Ref: types.PackageRef{
			Platform: types.PlatformSponge,
			Name:     input.ToProjectName(plugin.ID),
		},
		Version:  types.BareVersion(v),
		FilePath: localPath,
		Metadata: types.Metadata{
			Title:   fn.Ternary(plugin.Name != "", plugin.Name, plugin.ID),
			Brief:   plugin.Description,
			License: metadata.License,
			Authors: translateSpongeContributors(
				resolveSpongePluginContributors(metadata, plugin),
			),
			Urls: translateSpongeLinks(
				resolveSpongePluginLinks(metadata, plugin),
			),
		},
	}

	deps := translateSpongeDependencies(
		resolveSpongePluginDependencies(metadata, plugin),
	)
	if len(deps) > 0 {
		info.Dependencies = deps
	}

	return info, true
}

func resolveSpongePluginVersion(
	metadata *fileschema.FileSpongePluginsIdentifier,
	plugin fileschema.FileSpongePluginMetadata,
) string {
	if v := strings.TrimSpace(plugin.Version); v != "" {
		return v
	}
	return strings.TrimSpace(metadata.Global.Version)
}

func resolveSpongePluginLinks(
	metadata *fileschema.FileSpongePluginsIdentifier,
	plugin fileschema.FileSpongePluginMetadata,
) struct {
	Homepage string
	Source   string
	Issues   string
} {
	links := struct {
		Homepage string
		Source   string
		Issues   string
	}{
		Homepage: metadata.Global.Links.Homepage,
		Source:   metadata.Global.Links.Source,
		Issues:   metadata.Global.Links.Issues,
	}
	if strings.TrimSpace(plugin.Links.Homepage) != "" {
		links.Homepage = plugin.Links.Homepage
	}
	if strings.TrimSpace(plugin.Links.Source) != "" {
		links.Source = plugin.Links.Source
	}
	if strings.TrimSpace(plugin.Links.Issues) != "" {
		links.Issues = plugin.Links.Issues
	}
	return links
}

func resolveSpongePluginContributors(
	metadata *fileschema.FileSpongePluginsIdentifier,
	plugin fileschema.FileSpongePluginMetadata,
) []struct {
	Name        string
	Description string
} {
	contributors := metadata.Global.Contributors
	if len(plugin.Contributors) > 0 {
		contributors = plugin.Contributors
	}
	resolved := make(
		[]struct {
			Name        string
			Description string
		}, 0, len(contributors),
	)
	for _, contributor := range contributors {
		resolved = append(
			resolved, struct {
				Name        string
				Description string
			}{
				Name:        contributor.Name,
				Description: contributor.Description,
			},
		)
	}
	return resolved
}

func resolveSpongePluginDependencies(
	metadata *fileschema.FileSpongePluginsIdentifier,
	plugin fileschema.FileSpongePluginMetadata,
) []struct {
	ID        string
	Version   string
	LoadOrder string
	Optional  bool
} {
	deps := metadata.Global.Dependencies
	if len(plugin.Dependencies) > 0 {
		deps = plugin.Dependencies
	}
	resolved := make(
		[]struct {
			ID        string
			Version   string
			LoadOrder string
			Optional  bool
		}, 0, len(deps),
	)
	for _, dep := range deps {
		resolved = append(
			resolved, struct {
				ID        string
				Version   string
				LoadOrder string
				Optional  bool
			}{
				ID:        dep.ID,
				Version:   dep.Version,
				LoadOrder: dep.LoadOrder,
				Optional:  dep.Optional,
			},
		)
	}
	return resolved
}

func translateSpongeContributors(
	contributors []struct {
		Name        string
		Description string
	},
) []types.Person {
	people := make([]types.Person, 0, len(contributors))
	for _, contributor := range contributors {
		if strings.TrimSpace(contributor.Name) == "" {
			continue
		}
		people = append(
			people, types.Person{
				Name: contributor.Name,
				Role: contributor.Description,
			},
		)
	}
	return people
}

func translateSpongeLinks(
	links struct {
		Homepage string
		Source   string
		Issues   string
	},
) []types.Url {
	urls := make([]types.Url, 0, 3)
	if homepage := strings.TrimSpace(links.Homepage); homepage != "" {
		urls = append(
			urls,
			types.Url{Name: "Homepage", Type: types.UrlHome, Url: homepage},
		)
	}
	if source := strings.TrimSpace(links.Source); source != "" {
		urls = append(
			urls,
			types.Url{Name: "Source", Type: types.UrlSource, Url: source},
		)
	}
	if issues := strings.TrimSpace(links.Issues); issues != "" {
		urls = append(
			urls,
			types.Url{Name: "Issues", Type: types.UrlIssues, Url: issues},
		)
	}
	return urls
}

func translateSpongeDependencies(
	deps []struct {
		ID        string
		Version   string
		LoadOrder string
		Optional  bool
	},
) []ArtifactDep {
	translated := make([]ArtifactDep, 0, len(deps))
	for _, dep := range deps {
		id := strings.TrimSpace(dep.ID)
		v := strings.TrimSpace(dep.Version)
		if id == "" || v == "" || strings.EqualFold(id, "spongeapi") {
			continue
		}
		translated = append(
			translated, ArtifactDep{
				Ref: types.PackageRef{
					Platform: types.PlatformSponge,
					Name:     input.ToProjectName(id),
				},
				Constraint: version.ParseRange(
					v,
					version.DialectMavenRange,
					types.Maven,
				),
				Mandatory: !dep.Optional,
			},
		)
	}
	return translated
}
