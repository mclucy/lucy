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
	if results.Items[0].ProjectName != "HelpChat/PlaceholderAPI" {
		t.Fatalf(
			"expected first project HelpChat/PlaceholderAPI, got %s",
			results.Items[0].ProjectName,
		)
	}
	if results.Items[1].ProjectName != "Alfie51m/PronounsMC" {
		t.Fatalf(
			"expected second project Alfie51m/PronounsMC, got %s",
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

// TestHangarCandidateDownloadSelection covers the live Candidates download
// boundary: explicit platforms never fall back, and a platform-less request
// follows the documented preferred order while preserving URL, filename, and
// SHA-256 metadata.
func TestHangarCandidateDownloadSelection(t *testing.T) {
	version := &hangarVersion{
		Name: "2.12.3",
		Downloads: map[string]hangarDownload{
			"PAPER": {
				DownloadURL: "https://hangarcdn.papermc.io/plugins/HelpChat/PlaceholderAPI/versions/2.12.3/PAPER/PlaceholderAPI-2.12.3-paper.jar",
				FileInfo: hangarFileInfo{
					Name:       "PlaceholderAPI-2.12.3-paper.jar",
					SHA256Hash: "7b5c9ca9e9194f8aeb1dbee1d7cc3cd54bbaacac32e2e71141504127637d0133",
				},
			},
			"WATERFALL": {
				DownloadURL: "https://hangarcdn.papermc.io/plugins/HelpChat/PlaceholderAPI/versions/2.12.3/WATERFALL/PlaceholderAPI-2.12.3-waterfall.jar",
				FileInfo: hangarFileInfo{
					Name:       "PlaceholderAPI-2.12.3-waterfall.jar",
					SHA256Hash: "067ed70571c4561981e318f915533259d168f2bf56210c84c82031c7a5a2af37",
				},
			},
		},
	}

	platform, download, ok := version.candidateDownload(
		[]string{hangarPlatformWaterfall},
	)
	if !ok || platform != hangarPlatformWaterfall {
		t.Fatalf(
			"expected explicit waterfall selection, got platform=%q ok=%v",
			platform,
			ok,
		)
	}
	if download.URL() != version.Downloads["WATERFALL"].DownloadURL {
		t.Fatalf("expected waterfall url, got %q", download.URL())
	}
	if download.metadataFilename() != "PlaceholderAPI-2.12.3-waterfall.jar" {
		t.Fatalf(
			"expected waterfall filename, got %q",
			download.metadataFilename(),
		)
	}
	if got := download.SHA256(); got != version.Downloads["WATERFALL"].FileInfo.SHA256Hash {
		t.Fatalf("expected waterfall sha256, got %q", got)
	}

	platform, download, ok = version.candidateDownload([]string{"VELOCITY"})
	if ok {
		t.Fatalf(
			"expected missing platform not to fall back, got %q (%q)",
			platform,
			download.metadataFilename(),
		)
	}

	platform, download, ok = version.candidateDownload(nil)
	if !ok || platform != hangarPlatformPaper {
		t.Fatalf(
			"expected platform-less request to prefer paper, got platform=%q ok=%v",
			platform,
			ok,
		)
	}
	if download.metadataFilename() != "PlaceholderAPI-2.12.3-paper.jar" {
		t.Fatalf("expected paper filename, got %q", download.metadataFilename())
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
