package hangar

import (
	"slices"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestProjectSearchResponseToSearchResults(t *testing.T) {
	resp := &projectSearchResponse{
		Pagination: hangarPagination{Count: 2, Limit: 2, Offset: 0},
		Result: []hangarProject{
			{
				Namespace: hangarProjectNamespace{
					Owner: "HelpChat", Slug: "PlaceholderAPI",
				},
			},
			{
				Namespace: hangarProjectNamespace{
					Owner: "Alfie51m", Slug: "PronounsMC",
				},
			},
		},
	}

	results := resp.ToSearchResults()

	if results.Source != types.SourceHangar {
		t.Fatalf("expected Hangar source, got %v", results.Source)
	}
	if len(results.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(results.Projects))
	}
	if results.Projects[0] != types.PackageName("placeholderapi") {
		t.Fatalf(
			"expected first project placeholderapi, got %s",
			results.Projects[0],
		)
	}
	if results.Projects[1] != types.PackageName("pronounsmc") {
		t.Fatalf(
			"expected second project pronounsmc, got %s",
			results.Projects[1],
		)
	}

	ref := resp.Result[0].ProjectRef()
	if ref.LookupPath() != "HelpChat/PlaceholderAPI" {
		t.Fatalf("expected owner-aware lookup path, got %s", ref.LookupPath())
	}
	if ref.CanonicalName() != types.PackageName("placeholderapi") {
		t.Fatalf(
			"expected canonical project name placeholderapi, got %s",
			ref.CanonicalName(),
		)
	}
}

func TestHangarProjectToProjectInformationAndSupport(t *testing.T) {
	project := &hangarProject{
		Name:        "PlaceholderAPI",
		Description: "A resource that allows information from your favorite plugins be shown practically anywhere!",
		Namespace: hangarProjectNamespace{
			Owner: "HelpChat", Slug: "PlaceholderAPI",
		},
		Settings: hangarProjectSettings{
			License: hangarProjectLicense{Name: "GPL"},
			Links: []hangarLinkSection{
				{
					Links: []hangarLink{
						{
							Name: "Issues",
							URL:  "https://github.com/PlaceholderAPI/PlaceholderAPI/issues",
						}, {
							Name: "Source",
							URL:  "https://github.com/PlaceholderAPI/PlaceholderAPI",
						}, {
							Name: "Support",
							URL:  "https://helpch.at/discord",
						}, {
							Name: "Wiki",
							URL:  "https://github.com/PlaceholderAPI/PlaceholderAPI/wiki",
						},
					},
				},
			},
		},
		SupportedPlatforms: hangarPlatformVersionMap{
			"PAPER": {"1.20.6", "1.21", "1.21.1"},
		},
		MainPageContent: "# PlaceholderAPI\n\nUse placeholders everywhere.",
		MemberNames:     []string{"glare", "funnycube", "helpchat"},
	}

	info := project.ToProjectInformation()
	if info.Title != "PlaceholderAPI" {
		t.Fatalf("expected title PlaceholderAPI, got %s", info.Title)
	}
	if info.Brief == "" || info.Brief != project.Description {
		t.Fatalf("expected brief to preserve description, got %q", info.Brief)
	}
	if info.Description != project.MainPageContent {
		t.Fatalf("expected markdown body to map to description")
	}
	if !info.DescriptionIsMarkdown {
		t.Fatalf("expected markdown body to be detected")
	}
	if info.License != "GPL" {
		t.Fatalf("expected GPL license, got %s", info.License)
	}
	if len(info.Authors) != 3 || info.Authors[1].Name != "funnycube" {
		t.Fatalf(
			"expected member names to map to authors, got %+v",
			info.Authors,
		)
	}
	if len(info.Urls) < 5 {
		t.Fatalf(
			"expected project and settings links to map to info URLs, got %d",
			len(info.Urls),
		)
	}
	assertHasHangarURL(
		t,
		info.Urls,
		"Hangar",
		types.UrlHome,
		"https://hangar.papermc.io/HelpChat/PlaceholderAPI",
	)
	assertHasHangarURL(
		t,
		info.Urls,
		"Issues",
		types.UrlIssues,
		"https://github.com/PlaceholderAPI/PlaceholderAPI/issues",
	)
	assertHasHangarURL(
		t,
		info.Urls,
		"Source",
		types.UrlSource,
		"https://github.com/PlaceholderAPI/PlaceholderAPI",
	)
	assertHasHangarURL(
		t,
		info.Urls,
		"Support",
		types.UrlForum,
		"https://helpch.at/discord",
	)
	assertHasHangarURL(
		t,
		info.Urls,
		"Wiki",
		types.UrlWiki,
		"https://github.com/PlaceholderAPI/PlaceholderAPI/wiki",
	)

	support := project.ToProjectSupport()
	if len(support.Platforms) != 1 || support.Platforms[0] != types.Platform("paper") {
		t.Fatalf("expected paper support, got %+v", support.Platforms)
	}
	if len(support.MinecraftVersions) != 3 || support.MinecraftVersions[0] != types.BareVersion("1.20.6") {
		t.Fatalf(
			"expected paper minecraft versions to be preserved, got %+v",
			support.MinecraftVersions,
		)
	}
	if !support.Authentic {
		t.Fatalf("expected Hangar support metadata to be authentic")
	}
}

func TestHangarVersionToPackageRemoteAndSupport(t *testing.T) {
	version := &hangarVersion{
		Name: "2.12.2",
		Downloads: map[string]hangarDownload{
			"PAPER": {
				DownloadURL: "https://hangarcdn.papermc.io/plugins/HelpChat/PlaceholderAPI/versions/2.12.2/PAPER/PlaceholderAPI-2.12.2.jar",
				FileInfo: hangarFileInfo{
					Name:       "PlaceholderAPI-2.12.2.jar",
					SHA256Hash: "ff76af20c7acf327ff2a28fb2dbd6694e3f946503e72635a5f7b6cb2e64fc014",
				},
			},
		},
		PlatformDependencies: hangarPlatformVersionMap{
			"PAPER": {"1.20.6", "1.21", "1.21.1"},
		},
		PluginDependencies: map[string][]hangarPluginDependency{},
	}

	remote, ok := version.ToPackageRemoteForPlatform(types.Platform("paper"))
	if !ok {
		t.Fatalf("expected PAPER download to resolve")
	}
	if remote.Source != types.SourceHangar {
		t.Fatalf("expected Hangar source, got %v", remote.Source)
	}
	if remote.FileUrl != version.Downloads["PAPER"].DownloadURL {
		t.Fatalf(
			"expected download URL to be preserved, got %s",
			remote.FileUrl,
		)
	}
	if remote.Filename != "PlaceholderAPI-2.12.2.jar" {
		t.Fatalf("expected filename to be preserved, got %s", remote.Filename)
	}
	if remote.HashAlgorithm != "sha256" {
		t.Fatalf("expected sha256 algorithm, got %s", remote.HashAlgorithm)
	}
	if remote.Hash != version.Downloads["PAPER"].FileInfo.SHA256Hash {
		t.Fatalf("expected sha256 hash to be preserved, got %s", remote.Hash)
	}

	support := version.ToProjectSupport()
	if len(support.Platforms) != 1 || support.Platforms[0] != types.Platform("paper") {
		t.Fatalf(
			"expected version support for paper, got %+v",
			support.Platforms,
		)
	}
	if len(version.PluginDependencyNames()) != 0 {
		t.Fatalf(
			"expected no plugin dependency names, got %+v",
			version.PluginDependencyNames(),
		)
	}
}

func TestHangarVersionRemoteSelectionPolicy(t *testing.T) {
	external := "https://downloads.example.com/placeholderapi-bukkit.jar"
	version := &hangarVersion{
		Name: "2.12.3",
		Downloads: map[string]hangarDownload{
			"PAPER": {
				DownloadURL: "https://hangarcdn.papermc.io/plugins/HelpChat/PlaceholderAPI/versions/2.12.3/PAPER/PlaceholderAPI-2.12.3-paper.jar",
				FileInfo: hangarFileInfo{
					Name:       "PlaceholderAPI-2.12.3-paper.jar",
					SHA256Hash: "paper-sha256",
				},
			},
			"BUKKIT": {
				DownloadURL: "https://hangarcdn.papermc.io/plugins/HelpChat/PlaceholderAPI/versions/2.12.3/BUKKIT/PlaceholderAPI-2.12.3-bukkit.jar",
				ExternalURL: &external,
				FileInfo: hangarFileInfo{
					Name:       "PlaceholderAPI-2.12.3-bukkit.jar",
					SHA256Hash: "bukkit-sha256",
				},
			},
		},
		PlatformDependencies: hangarPlatformVersionMap{
			"PAPER":  {"1.21.1", "1.21"},
			"BUKKIT": {"1.21"},
		},
		PluginDependencies: map[string][]hangarPluginDependency{
			"PAPER": {
				{Name: "Vault", Required: true},
				{Name: "EssentialsX", Required: false},
			},
		},
	}

	defaultRemote := version.ToPackageRemote()
	if defaultRemote.FileUrl == "" {
		t.Fatalf("expected ambiguous remote selection to still resolve a fetchable artifact")
	}
	if defaultRemote.Filename == "" {
		t.Fatalf("expected ambiguous remote selection to keep a filename")
	}
	if defaultRemote.Hash == "" || defaultRemote.HashAlgorithm != "sha256" {
		t.Fatalf(
			"expected ambiguous remote selection to preserve sha256 metadata, got hash=%q algo=%q",
			defaultRemote.Hash,
			defaultRemote.HashAlgorithm,
		)
	}

	paperRemote, ok := version.ToPackageRemoteForPlatform(types.Platform("PaPeR"))
	if !ok {
		t.Fatalf("expected case-insensitive platform match for paper")
	}
	if paperRemote.FileUrl != version.Downloads["PAPER"].DownloadURL {
		t.Fatalf("expected paper download url, got %q", paperRemote.FileUrl)
	}

	if _, ok := version.ToPackageRemoteForPlatform(types.Platform("velocity")); ok {
		t.Fatalf("expected missing platform to remain unresolved")
	}

	deps := version.PluginDependencyNames()
	if len(deps) != 2 {
		t.Fatalf(
			"expected two normalized plugin dependency names, got %#v",
			deps,
		)
	}
	if !slices.Contains(
		deps,
		types.PackageName("essentialsx"),
	) || !slices.Contains(deps, types.PackageName("vault")) {
		t.Fatalf("expected normalized plugin dependency names, got %#v", deps)
	}

	support := version.ToProjectSupport()
	if len(support.Platforms) != 2 || support.Platforms[0] != "bukkit" || support.Platforms[1] != "paper" {
		t.Fatalf(
			"expected sorted provider platforms, got %#v",
			support.Platforms,
		)
	}
	if len(support.MinecraftVersions) != 2 || support.MinecraftVersions[0] != "1.21" || support.MinecraftVersions[1] != "1.21.1" {
		t.Fatalf(
			"expected deduplicated minecraft versions, got %#v",
			support.MinecraftVersions,
		)
	}
}

func TestVersionListAndPlatformVersionScaffolding(t *testing.T) {
	versions := HangarVersionListResponse{
		Pagination: hangarPagination{Count: 1, Limit: 1, Offset: 0},
		Result: []hangarVersion{
			{
				Name: "2.12.2",
				Downloads: map[string]hangarDownload{
					"PAPER": {
						DownloadURL: "https://example.invalid/plugin.jar",
						FileInfo:    hangarFileInfo{Name: "plugin.jar"},
					},
				},
			},
		},
	}

	if got := versions.Result[0].ToPackageRemote().Filename; got != "plugin.jar" {
		t.Fatalf(
			"expected version list entry to still use version conversion scaffolding, got %s",
			got,
		)
	}

	platformVersions := []HangarPlatformVersion{
		{
			Version:     "1.21",
			SubVersions: []string{"1.21.1", "1.21"},
		},
	}
	if len(platformVersions[0].SubVersions) != 2 {
		t.Fatalf("expected platform version scaffolding to preserve subversions")
	}
}

func assertHasHangarURL(
t *testing.T,
urls []types.Url,
name string,
kind types.UrlType,
value string,
) {
	t.Helper()
	if slices.ContainsFunc(
		urls, func(u types.Url) bool {
			if u.Name == name && u.Type == kind && u.Url == value {
				return true
			}
			return false
		},
	) {
		return
	}
	t.Fatalf(
		"missing url name=%q type=%v value=%q in %#v",
		name,
		kind,
		value,
		urls,
	)
}
