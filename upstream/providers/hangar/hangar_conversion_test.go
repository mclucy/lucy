package hangar

import (
	"slices"
	"testing"

	"github.com/mclucy/lucy/catalog"
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

	results := resp.ToSearchResults(catalog.ProviderHangar)

	if results.Provider != catalog.ProviderHangar {
		t.Fatalf("expected Hangar source, got %v", results.Provider)
	}
	if len(results.Items) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(results.Items))
	}
	if results.Items[0].ProjectName != "placeholderapi" {
		t.Fatalf(
			"expected first project placeholderapi, got %s",
			results.Items[0].ProjectName,
		)
	}
	if results.Items[1].ProjectName != "pronounsmc" {
		t.Fatalf(
			"expected second project pronounsmc, got %s",
			results.Items[1].ProjectName,
		)
	}

	ref := resp.Result[0].ProjectRef()
	if ref.LookupPath() != "HelpChat/PlaceholderAPI" {
		t.Fatalf("expected owner-aware lookup path, got %s", ref.LookupPath())
	}
	if ref.CanonicalName() != "placeholderapi" {
		t.Fatalf(
			"expected canonical project name placeholderapi, got %s",
			ref.CanonicalName(),
		)
	}
}

func TestHangarProjectToProjectInformation(t *testing.T) {
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
	if len(info.URLs) < 5 {
		t.Fatalf(
			"expected project and settings links to map to info URLs, got %d",
			len(info.URLs),
		)
	}
	assertHasHangarURL(
		t,
		info.URLs,
		"Hangar",
		catalog.URLHome,
		"https://hangar.papermc.io/HelpChat/PlaceholderAPI",
	)
	assertHasHangarURL(
		t,
		info.URLs,
		"Issues",
		catalog.URLIssues,
		"https://github.com/PlaceholderAPI/PlaceholderAPI/issues",
	)
	assertHasHangarURL(
		t,
		info.URLs,
		"Source",
		catalog.URLSource,
		"https://github.com/PlaceholderAPI/PlaceholderAPI",
	)
	assertHasHangarURL(
		t,
		info.URLs,
		"Support",
		catalog.URLForum,
		"https://helpch.at/discord",
	)
	assertHasHangarURL(
		t,
		info.URLs,
		"Wiki",
		catalog.URLWiki,
		"https://github.com/PlaceholderAPI/PlaceholderAPI/wiki",
	)
}

func TestHangarVersionToPackageRemote(t *testing.T) {
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

	remote, ok := version.ToPackageRemoteForPlatform("paper")
	if !ok {
		t.Fatalf("expected PAPER download to resolve")
	}
	if remote.ID.Provider != catalog.ProviderHangar {
		t.Fatalf("expected Hangar source, got %v", remote.ID.Provider)
	}
	if remote.FileURL != version.Downloads["PAPER"].DownloadURL {
		t.Fatalf(
			"expected download URL to be preserved, got %s",
			remote.FileURL,
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

	if len(version.PluginDependencyNames()) != 0 {
		t.Fatalf(
			"expected no plugin dependency names, got %+v",
			version.PluginDependencyNames(),
		)
	}
}

func TestHangarVersionRemoteSelectionPolicy(t *testing.T) {
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
				ExternalURL: new("https://downloads.example.com/placeholderapi-bukkit.jar"),
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
	if defaultRemote.FileURL == "" {
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

	paperRemote, ok := version.ToPackageRemoteForPlatform("PaPeR")
	if !ok {
		t.Fatalf("expected case-insensitive platform match for paper")
	}
	if paperRemote.FileURL != version.Downloads["PAPER"].DownloadURL {
		t.Fatalf("expected paper download url, got %q", paperRemote.FileURL)
	}

	if _, ok := version.ToPackageRemoteForPlatform("velocity"); ok {
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
		"essentialsx",
	) || !slices.Contains(deps, "vault") {
		t.Fatalf("expected normalized plugin dependency names, got %#v", deps)
	}
}

func assertHasHangarURL(
	t *testing.T,
	urls []catalog.URL,
	name string,
	kind catalog.URLType,
	value string,
) {
	t.Helper()
	if slices.ContainsFunc(
		urls, func(u catalog.URL) bool {
			if u.Name == name && u.Type == kind && u.URL == value {
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
