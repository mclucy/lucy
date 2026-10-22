package artifact

import (
	"archive/zip"
	"encoding/json/v2"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/input"
	"github.com/mclucy/lucy/internal/fileschema"
	"github.com/mclucy/lucy/internal/fn"
	"github.com/mclucy/lucy/log"
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
) ([]PackageInfo, error) {
	for _, f := range zipRdr.File {
		if f.Name != spongePluginMetadataPath {
			continue
		}

		rc, err := f.Open()
		if err != nil {
			return nil, err
		}

		var metadata fileschema.FileSpongePluginsIdentifier
		decodeErr := json.UnmarshalRead(rc, &metadata)
		fn.CloseReader(rc, log.Warn)
		if decodeErr != nil {
			return nil, decodeErr
		}

		if !validSpongeMetadata(&metadata) {
			return []PackageInfo{}, nil
		}

		infos := make([]PackageInfo, 0, len(metadata.Plugins))
		for _, plugin := range metadata.Plugins {
			info, ok, err := translateSpongePlugin(&metadata, plugin, filePath)
			if err != nil {
				return nil, err
			}
			if !ok {
				continue
			}
			infos = append(infos, info)
		}

		if len(infos) == 0 {
			return []PackageInfo{}, nil
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
) (PackageInfo, bool, error) {
	if !hasConcreteSpongePluginIdentity(metadata, plugin) {
		return PackageInfo{}, false, nil
	}

	v := resolveSpongePluginVersion(metadata, plugin)
	info := PackageInfo{
		Ref:      catalog.VersionedRef{Ref: catalog.Ref{Name: input.ToProjectName(plugin.ID), Provider: catalog.ProviderUnknown}, Ecosystem: catalog.EcosystemSponge, Version: version.Version(v)},
		FilePath: localPath,
		Metadata: catalog.Metadata{
			Title:   fn.Ternary(plugin.Name != "", plugin.Name, plugin.ID),
			Brief:   plugin.Description,
			License: metadata.License,
			Authors: translateSpongeContributors(
				resolveSpongePluginContributors(metadata, plugin),
			),
			URLs: translateSpongeLinks(
				resolveSpongePluginLinks(metadata, plugin),
			),
		},
	}

	deps, err := translateSpongeDependencies(
		resolveSpongePluginDependencies(metadata, plugin),
	)
	if err != nil {
		return PackageInfo{}, false, err
	}
	if len(deps) > 0 {
		info.Dependencies = deps
	}

	return info, true, nil
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
) []catalog.Person {
	people := make([]catalog.Person, 0, len(contributors))
	for _, contributor := range contributors {
		if strings.TrimSpace(contributor.Name) == "" {
			continue
		}
		people = append(
			people, catalog.Person{
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
) []catalog.URL {
	urls := make([]catalog.URL, 0, 3)
	if homepage := strings.TrimSpace(links.Homepage); homepage != "" {
		urls = append(
			urls,
			catalog.URL{Name: "Homepage", Type: catalog.URLHome, URL: homepage},
		)
	}
	if source := strings.TrimSpace(links.Source); source != "" {
		urls = append(
			urls,
			catalog.URL{Name: "Source", Type: catalog.URLSource, URL: source},
		)
	}
	if issues := strings.TrimSpace(links.Issues); issues != "" {
		urls = append(
			urls,
			catalog.URL{Name: "Issues", Type: catalog.URLIssues, URL: issues},
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
) ([]Dependency, error) {
	translated := make([]Dependency, 0, len(deps))
	for _, dep := range deps {
		id := strings.TrimSpace(dep.ID)
		v := strings.TrimSpace(dep.Version)
		if id == "" || v == "" || strings.EqualFold(id, "spongeapi") {
			continue
		}
		constraint, err := version.ParseConstraint(v, version.DialectMaven)
		if err != nil {
			return nil, err
		}
		translated = append(
			translated, Dependency{
				Ref:        catalog.VersionedRef{Ref: catalog.Ref{Name: input.ToProjectName(id), Provider: catalog.ProviderUnknown}, Ecosystem: catalog.EcosystemSponge},
				Constraint: constraint,
				Mandatory:  !dep.Optional,
			},
		)
	}
	return translated, nil
}
