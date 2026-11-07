package spiget

import (
	"encoding/base64"
	"testing"

	"github.com/mclucy/lucy/catalog"
)

func TestSearchResponseToSearchResults(t *testing.T) {
	resp := searchResponse{
		{ID: 6245, Name: "PlaceholderAPI"},
		{ID: 3031, Name: "ClearChat 3.2"},
	}

	results := resp.ToSearchResults(catalog.ProviderSpiget)

	if results.Provider != catalog.ProviderSpiget {
		t.Fatalf(
			"expected source %v, got %v",
			catalog.ProviderSpiget,
			results.Provider,
		)
	}
	if len(results.Items) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(results.Items))
	}
	if results.Items[0].ProjectName != "6245" {
		t.Fatalf("expected numeric resource identity, got %q", results.Items[0].ProjectName)
	}
	if results.Items[0].Title != "PlaceholderAPI" {
		t.Fatalf("expected display title PlaceholderAPI, got %q", results.Items[0].Title)
	}
	if results.Items[1].ProjectName != "3031" {
		t.Fatalf("expected numeric resource identity, got %q", results.Items[1].ProjectName)
	}
	if results.Items[1].Title != "ClearChat 3.2" {
		t.Fatalf("expected display title ClearChat 3.2, got %q", results.Items[1].Title)
	}
}

func TestProjectInfoConversion(t *testing.T) {
	resource := resourceResponse{
		ID:            6245,
		Name:          "PlaceholderAPI",
		Tag:           "Server placeholder expansion API",
		Description:   base64.StdEncoding.EncodeToString([]byte("<p>Main description</p>")),
		Documentation: base64.StdEncoding.EncodeToString([]byte("<p>Documentation tab</p>")),
		Links: map[string]string{
			"additionalInformation": "https://wiki.placeholderapi.com/",
			"discussion":            "threads/placeholderapi.61918/",
		},
		SourceCodeLink: "https://github.com/PlaceholderAPI/PlaceholderAPI",
		DonationLink:   "https://github.com/sponsors/PlaceholderAPI",
	}

	info := resource.ToProjectInformation()

	if info.Title != "PlaceholderAPI" {
		t.Fatalf("expected title PlaceholderAPI, got %q", info.Title)
	}
	if info.Brief != "Server placeholder expansion API" {
		t.Fatalf("expected brief to match tag, got %q", info.Brief)
	}
	if info.Description != "<p>Main description</p>\n\n<hr />\n\n<h2>Documentation</h2>\n<p>Documentation tab</p>" {
		t.Fatalf("unexpected combined description: %q", info.Description)
	}
	if info.DescriptionIsMarkdown {
		t.Fatalf("expected html description not to be marked as markdown")
	}

	assertHasURL(
		t,
		info.URLs,
		"Additional information",
		catalog.URLHome,
		"https://wiki.placeholderapi.com/",
	)
	assertHasURL(
		t,
		info.URLs,
		"Discussion",
		catalog.URLForum,
		"https://www.spigotmc.org/threads/placeholderapi.61918/",
	)
	assertHasURL(
		t,
		info.URLs,
		"Source",
		catalog.URLSource,
		"https://github.com/PlaceholderAPI/PlaceholderAPI",
	)
	assertHasURL(
		t,
		info.URLs,
		"Donate",
		catalog.URLSponsor,
		"https://github.com/sponsors/PlaceholderAPI",
	)
}

func TestProjectInfoNormalizesLinks(t *testing.T) {
	resource := resourceResponse{
		Name: "PlaceholderAPI",
		Links: map[string]string{
			"alternativeSupport": "/threads/placeholderapi.61918/",
			"custom_docs":        "resources/placeholderapi.6245/",
			"":                   "https://ignored.example.com",
		},
	}

	info := resource.ToProjectInformation()

	assertHasURL(
		t,
		info.URLs,
		"Support",
		catalog.URLForum,
		"https://www.spigotmc.org/threads/placeholderapi.61918/",
	)
	assertHasURL(
		t,
		info.URLs,
		"Custom docs",
		catalog.URLMisc,
		"https://www.spigotmc.org/resources/placeholderapi.6245/",
	)
}

func TestProjectInfoDropsInvalidBase64(t *testing.T) {
	resource := resourceResponse{
		Name:          "Broken",
		Description:   "%%%not-base64%%%",
		Documentation: base64.StdEncoding.EncodeToString([]byte("<p>Still valid</p>")),
	}

	info := resource.ToProjectInformation()

	if info.Description != "<p>Still valid</p>" {
		t.Fatalf(
			"expected valid documentation to survive invalid description, got %q",
			info.Description,
		)
	}
	if info.DescriptionIsMarkdown {
		t.Fatalf("expected html-only fallback not to be markdown")
	}
	if decodeBase64HTML("%%%not-base64%%%").Valid {
		t.Fatalf("expected invalid base64 decode to be marked invalid")
	}
}

// TestSpigetCurrentDownloadArtifact covers the live Candidates download inputs:
// Spiget's resource-scoped current download URL and the filename derived from
// provider fields.
func TestSpigetCurrentDownloadArtifact(t *testing.T) {
	resource := resourceResponse{
		ID: 6245, Name: "PlaceholderAPI",
		File: resourceFileResponse{Type: ".jar"},
	}
	downloadURL := resourceDownloadURL(resource.ID)
	if downloadURL != "https://api.spiget.org/v2/resources/6245/download" {
		t.Fatalf("unexpected current download url %q", downloadURL)
	}
	filename := artifactFilename(
		resource.ID,
		normalizedProjectName(resource.Name).String(),
		"2.12.2",
		625258,
		resource.File.Type,
		downloadURL,
	)
	if filename != "placeholderapi-2.12.2.jar" {
		t.Fatalf("unexpected filename %q", filename)
	}
}

// TestSpigetExternalArtifactFilename covers the live Candidates filename rule
// for external downloads: the archive name comes from the external URL.
func TestSpigetExternalArtifactFilename(t *testing.T) {
	filename := artifactFilename(
		10,
		"external-plugin",
		"1.0.0",
		20,
		"external",
		"https://downloads.example.com/plugin.jar",
	)
	if filename != "plugin.jar" {
		t.Fatalf("expected external archive filename, got %q", filename)
	}
}

// TestSpigetVersionIdentity covers the live Candidates version label: the
// published human name wins, with the numeric version ID as fallback.
func TestSpigetVersionIdentity(t *testing.T) {
	if got := spigetVersion("2.12.2", 625258); got != "2.12.2" {
		t.Fatalf("expected human version, got %q", got)
	}
	if got := spigetVersion("", 625258); got != "625258" {
		t.Fatalf("expected numeric version fallback, got %q", got)
	}
}

func assertHasURL(
	t *testing.T,
	urls []catalog.URL,
	name string,
	kind catalog.URLType,
	value string,
) {
	t.Helper()
	for _, url := range urls {
		if url.Name == name && url.Type == kind && url.URL == value {
			return
		}
	}
	t.Fatalf(
		"missing url name=%q type=%v value=%q in %#v",
		name,
		kind,
		value,
		urls,
	)
}
