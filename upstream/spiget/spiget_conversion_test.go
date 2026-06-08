package spiget

import (
	"encoding/base64"
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestSearchResponseToSearchResults(t *testing.T) {
	resp := searchResponse{
		{ID: 6245, Name: "PlaceholderAPI"},
		{ID: 3031, Name: "ClearChat 3.2"},
	}

	results := resp.ToSearchResults()

	if results.Source != types.SourceSpiget {
		t.Fatalf(
			"expected source %v, got %v",
			types.SourceSpiget,
			results.Source,
		)
	}
	if len(results.Projects) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(results.Projects))
	}
	if results.Projects[0] != "placeholderapi" {
		t.Fatalf("expected placeholderapi, got %q", results.Projects[0])
	}
	if results.Projects[1] != "clearchat-3-2" {
		t.Fatalf("expected clearchat-3-2, got %q", results.Projects[1])
	}
}

func TestResourceResponseToProjectInformation(t *testing.T) {
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
		info.Urls,
		"Additional information",
		types.UrlHome,
		"https://wiki.placeholderapi.com/",
	)
	assertHasURL(
		t,
		info.Urls,
		"Discussion",
		types.UrlForum,
		"https://www.spigotmc.org/threads/placeholderapi.61918/",
	)
	assertHasURL(
		t,
		info.Urls,
		"Source",
		types.UrlSource,
		"https://github.com/PlaceholderAPI/PlaceholderAPI",
	)
	assertHasURL(
		t,
		info.Urls,
		"Donate",
		types.UrlSponsor,
		"https://github.com/sponsors/PlaceholderAPI",
	)
}

func TestResourceResponseToProjectInformation_NormalizesRelativeAndMiscLinks(t *testing.T) {
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
		info.Urls,
		"Support",
		types.UrlForum,
		"https://www.spigotmc.org/threads/placeholderapi.61918/",
	)
	assertHasURL(
		t,
		info.Urls,
		"Custom docs",
		types.UrlMisc,
		"https://www.spigotmc.org/resources/placeholderapi.6245/",
	)
}

func TestResourceResponseToProjectInformation_InvalidBase64IsDropped(t *testing.T) {
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

func TestResourceResponseToProjectSupport_TestedVersionsAreNonAuthentic(t *testing.T) {
	resource := resourceResponse{
		TestedVersions: []string{"1.20.6", "1.21"},
	}

	support := resource.ToProjectSupport()

	if support.Authentic {
		t.Fatalf("expected testedVersions mapping to remain non-authentic")
	}
	if len(support.Platforms) != 0 {
		t.Fatalf(
			"expected no platform claims from Spiget testedVersions, got %v",
			support.Platforms,
		)
	}
	if len(support.MinecraftVersions) != 2 || support.MinecraftVersions[0] != "1.20.6" || support.MinecraftVersions[1] != "1.21" {
		t.Fatalf(
			"unexpected minecraft versions: %#v",
			support.MinecraftVersions,
		)
	}
}

func TestResolvedVersionToPackageRemote(t *testing.T) {
	remote := NewResolvedVersion(
		resourceResponse{
			ID: 6245, Name: "PlaceholderAPI",
			File: resourceFileResponse{Type: ".jar"},
		},
		versionResponse{ID: 625258, Name: "2.12.2"},
	).ToPackageRemote()

	if remote.Source != types.SourceSpiget {
		t.Fatalf(
			"expected source %v, got %v",
			types.SourceSpiget,
			remote.Source,
		)
	}
	if remote.FileUrl != "https://api.spiget.org/v2/resources/6245/versions/625258/download" {
		t.Fatalf("unexpected file url %q", remote.FileUrl)
	}
	if remote.Filename != "placeholderapi-2.12.2.jar" {
		t.Fatalf("unexpected filename %q", remote.Filename)
	}
	if remote.Hash != "" || remote.HashAlgorithm != "" {
		t.Fatalf(
			"expected no hash metadata, got hash=%q algo=%q",
			remote.Hash,
			remote.HashAlgorithm,
		)
	}
}

func TestResolvedVersion_ToPackageRemote_UsesExternalDownload(t *testing.T) {
	remote := resolvedVersion{
		ResourceID:  10,
		VersionID:   20,
		VersionName: "1.0.0",
		ProjectName: "external-plugin",
		FileType:    "external",
		External:    true,
		ExternalURL: "https://downloads.example.com/plugin.jar",
	}.ToPackageRemote()

	if remote.FileUrl != "https://downloads.example.com/plugin.jar" {
		t.Fatalf("expected external url, got %q", remote.FileUrl)
	}
	if remote.Filename != "external-plugin-1.0.0" {
		t.Fatalf("unexpected filename %q", remote.Filename)
	}
}

func TestResolvedVersionIdentityPolicy(t *testing.T) {
	resolved := NewResolvedVersion(
		resourceResponse{ID: 6245, Name: "PlaceholderAPI"},
		versionResponse{ID: 625258, Name: "2.12.2"},
	)

	if got := resolved.LucyVersion(); got != types.BareVersion("2.12.2") {
		t.Fatalf(
			"expected human version to drive lucy exact version, got %q",
			got,
		)
	}
	if !resolved.Matches(types.BareVersion("2.12.2")) {
		t.Fatalf("expected exact human version match")
	}
	if !resolved.Matches(types.BareVersion("625258")) {
		t.Fatalf("expected numeric version-id fallback match")
	}
	if resolved.Matches(types.BareVersion("2.12.1")) {
		t.Fatalf("did not expect mismatched version to match")
	}
	if resolved.Matches(types.VersionLatest) {
		t.Fatalf("did not expect latest alias to count as an exact resolved version")
	}
	if resolved.Matches(types.VersionCompatible) {
		t.Fatalf("did not expect compatible alias to count as an exact resolved version")
	}
}

func TestResolvedVersionLatestFallbackPolicy(t *testing.T) {
	resolved := NewResolvedVersion(
		resourceResponse{
			ID: 6245, Name: "PlaceholderAPI",
			File: resourceFileResponse{Type: "jar"},
		},
		versionResponse{ID: 625258},
	)

	if got := resolved.LucyVersion(); got != types.BareVersion("625258") {
		t.Fatalf(
			"expected numeric version id fallback for latest/exact resolution, got %q",
			got,
		)
	}
	if !resolved.Matches(types.BareVersion("625258")) {
		t.Fatalf("expected numeric version id to remain matchable")
	}

	remote := resolved.ToPackageRemote()
	if remote.FileUrl != "https://api.spiget.org/v2/resources/6245/versions/625258/download" {
		t.Fatalf("unexpected fallback file url %q", remote.FileUrl)
	}
	if remote.Filename != "placeholderapi-625258.jar" {
		t.Fatalf("unexpected numeric fallback filename %q", remote.Filename)
	}
}

func assertHasURL(
t *testing.T,
urls []types.Url,
name string,
kind types.UrlType,
value string,
) {
	t.Helper()
	for _, url := range urls {
		if url.Name == name && url.Type == kind && url.Url == value {
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
