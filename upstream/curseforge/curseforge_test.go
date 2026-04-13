package curseforge

import (
	"testing"

	"github.com/mclucy/lucy/types"
)

func TestSearchResponseToSearchResults(t *testing.T) {
	resp := &searchResponse{
		Data: []modResponse{
			{Slug: "jei"},
			{Slug: "just-enough-items"},
			{Slug: "rei"},
		},
	}

	results := resp.ToSearchResults()

	if results.Source != types.SourceCurseForge {
		t.Errorf("expected source CurseForge, got %v", results.Source)
	}
	if len(results.Projects) != 3 {
		t.Fatalf("expected 3 projects, got %d", len(results.Projects))
	}
	if results.Projects[0] != "jei" {
		t.Errorf("expected 'jei', got '%s'", results.Projects[0])
	}
	if results.Projects[1] != "just-enough-items" {
		t.Errorf("expected 'just-enough-items', got '%s'", results.Projects[1])
	}
	if results.Projects[2] != "rei" {
		t.Errorf("expected 'rei', got '%s'", results.Projects[2])
	}
}

func TestSearchResponseToSearchResults_Empty(t *testing.T) {
	resp := &searchResponse{
		Data: []modResponse{},
	}

	results := resp.ToSearchResults()

	if results.Source != types.SourceCurseForge {
		t.Errorf("expected source CurseForge, got %v", results.Source)
	}
	if len(results.Projects) != 0 {
		t.Errorf("expected 0 projects, got %d", len(results.Projects))
	}
}

func TestModResponseToProjectInformation(t *testing.T) {
	mod := &modResponse{
		Name:    "Just Enough Items",
		Summary: "JEI - View Items and Recipes",
		Links: modLinks{
			WebsiteUrl: "https://curseforge.com/minecraft/jei",
			WikiUrl:    "https://wiki.example.com/jei",
			IssuesUrl:  "https://github.com/jei/issues",
			SourceUrl:  "https://github.com/jei",
		},
		Authors: []modAuthor{
			{Name: "mezz", Url: "https://curseforge.com/members/mezz"},
		},
	}

	info := mod.ToProjectInformation()

	if info.Title != "Just Enough Items" {
		t.Errorf("expected title 'Just Enough Items', got '%s'", info.Title)
	}
	if info.Brief != "JEI - View Items and Recipes" {
		t.Errorf("expected brief, got '%s'", info.Brief)
	}
	if info.Description != "" {
		t.Errorf("expected empty description, got '%s'", info.Description)
	}
	if info.DescriptionIsMarkdown {
		t.Error("expected empty description not to be markdown")
	}
	if len(info.Urls) != 4 {
		t.Errorf("expected 4 URLs, got %d", len(info.Urls))
	}
	if len(info.Authors) != 1 {
		t.Errorf("expected 1 author, got %d", len(info.Authors))
	}
	if info.Authors[0].Name != "mezz" {
		t.Errorf("expected author 'mezz', got '%s'", info.Authors[0].Name)
	}
}

func TestModResponseToProjectInformation_EmptyLinks(t *testing.T) {
	mod := &modResponse{
		Name:    "Test Mod",
		Summary: "A test",
		Links:   modLinks{},
		Authors: []modAuthor{},
	}

	info := mod.ToProjectInformation()

	if len(info.Urls) != 0 {
		t.Errorf("expected 0 URLs for empty links, got %d", len(info.Urls))
	}
	if len(info.Authors) != 0 {
		t.Errorf("expected 0 authors, got %d", len(info.Authors))
	}
}

func TestModResponseToProjectInformation_PartialLinks(t *testing.T) {
	mod := &modResponse{
		Name:    "Partial",
		Summary: "Only some links",
		Links: modLinks{
			WebsiteUrl: "https://example.com",
			// WikiUrl, IssuesUrl, SourceUrl are empty
		},
		Authors: []modAuthor{},
	}

	info := mod.ToProjectInformation()

	if len(info.Urls) != 1 {
		t.Errorf("expected 1 URL, got %d", len(info.Urls))
	}
	if info.Urls[0].Type != types.UrlHome {
		t.Errorf("expected UrlHome, got %v", info.Urls[0].Type)
	}
}

func TestRawProjectInformationToProjectInformation(t *testing.T) {
	mod := &modResponse{
		Name:    "Markdown Mod",
		Summary: "Has long description",
	}

	info := rawProjectInformation{
		mod:         mod,
		description: "# Title\n\n- Item one\n- Item two",
	}.ToProjectInformation()

	if info.Description != "# Title\n\n- Item one\n- Item two" {
		t.Errorf("expected long description to be preserved, got '%s'", info.Description)
	}
	if !info.DescriptionIsMarkdown {
		t.Error("expected markdown-looking long description to be recognized")
	}
}

func TestFileResponseToPackageRemote_PrefersSha1(t *testing.T) {
	downloadUrl := "https://edge.forgecdn.net/files/12345/modfile.jar"
	f := &fileResponse{
		FileName:    "modfile.jar",
		DownloadUrl: &downloadUrl,
		Hashes: []fileHash{
			{Value: "abc123md5", Algo: 2},  // md5 listed first
			{Value: "def456sha1", Algo: 1}, // sha1 listed second
		},
	}

	remote := f.ToPackageRemote()

	if remote.Source != types.SourceCurseForge {
		t.Errorf("expected source CurseForge, got %v", remote.Source)
	}
	if remote.FileUrl != downloadUrl {
		t.Errorf("expected URL '%s', got '%s'", downloadUrl, remote.FileUrl)
	}
	if remote.Filename != "modfile.jar" {
		t.Errorf("expected filename 'modfile.jar', got '%s'", remote.Filename)
	}
	if remote.Hash != "def456sha1" {
		t.Errorf("expected sha1 hash 'def456sha1', got '%s'", remote.Hash)
	}
	if remote.HashAlgorithm != "sha1" {
		t.Errorf("expected 'sha1', got '%s'", remote.HashAlgorithm)
	}
}

func TestFileResponseToPackageRemote_NilDownloadUrl(t *testing.T) {
	f := &fileResponse{
		FileName:    "blocked-mod.jar",
		DownloadUrl: nil,
		Hashes: []fileHash{
			{Value: "abc123sha1", Algo: 1},
		},
	}

	remote := f.ToPackageRemote()

	if remote.FileUrl != "" {
		t.Errorf("expected empty FileUrl for nil downloadUrl, got '%s'", remote.FileUrl)
	}
	if remote.Hash != "abc123sha1" {
		t.Errorf("expected hash 'abc123sha1', got '%s'", remote.Hash)
	}
}

func TestFileResponseToPackageRemote_Md5Only(t *testing.T) {
	downloadUrl := "https://edge.forgecdn.net/files/12345/mod.jar"
	f := &fileResponse{
		FileName:    "mod.jar",
		DownloadUrl: &downloadUrl,
		Hashes: []fileHash{
			{Value: "md5hash", Algo: 2},
		},
	}

	remote := f.ToPackageRemote()

	if remote.Hash != "md5hash" {
		t.Errorf("expected md5 hash, got '%s'", remote.Hash)
	}
	if remote.HashAlgorithm != "md5" {
		t.Errorf("expected 'md5', got '%s'", remote.HashAlgorithm)
	}
}

func TestFileResponseToPackageRemote_NoHashes(t *testing.T) {
	downloadUrl := "https://edge.forgecdn.net/files/mod.jar"
	f := &fileResponse{
		FileName:    "mod.jar",
		DownloadUrl: &downloadUrl,
		Hashes:      []fileHash{},
	}

	remote := f.ToPackageRemote()

	if remote.Hash != "" {
		t.Errorf("expected empty hash, got '%s'", remote.Hash)
	}
	if remote.HashAlgorithm != "" {
		t.Errorf("expected empty hash algorithm, got '%s'", remote.HashAlgorithm)
	}
}

func TestModLoaderType(t *testing.T) {
	tests := []struct {
		platform types.Platform
		expected int
	}{
		{types.PlatformForge, 1},
		{types.PlatformFabric, 4},
		{types.PlatformNeoforge, 6},
		{types.PlatformAny, 0},
		{types.PlatformMCDR, 0},
	}

	for _, tt := range tests {
		got := modLoaderType(tt.platform)
		if got != tt.expected {
			t.Errorf("modLoaderType(%s) = %d, want %d",
				tt.platform, got, tt.expected)
		}
	}
}

func TestCurseforgeSearchSortField(t *testing.T) {
	tests := []struct {
		sort     types.SearchSort
		expected int
	}{
		{types.SearchSortRelevance, 2},
		{types.SearchSortDownloads, 6},
		{types.SearchSortNewest, 11},
		{types.SearchSortName, 4},
	}

	for _, tt := range tests {
		got := curseforgeSearchSortField(tt.sort)
		if got != tt.expected {
			t.Errorf("curseforgeSearchSortField(%s) = %d, want %d",
				tt.sort, got, tt.expected)
		}
	}
}

func TestSearchSortOrder(t *testing.T) {
	if got := searchSortOrder(types.SearchSortName); got != "asc" {
		t.Errorf("searchSortOrder(Name) = %s, want asc", got)
	}
	if got := searchSortOrder(types.SearchSortRelevance); got != "desc" {
		t.Errorf("searchSortOrder(Relevance) = %s, want desc", got)
	}
	if got := searchSortOrder(types.SearchSortDownloads); got != "desc" {
		t.Errorf("searchSortOrder(Downloads) = %s, want desc", got)
	}
	if got := searchSortOrder(types.SearchSortNewest); got != "desc" {
		t.Errorf("searchSortOrder(Newest) = %s, want desc", got)
	}
}

func TestSearchUrl_ContainsRequiredParams(t *testing.T) {
	options := types.SearchOptions{
		SortBy:         types.SearchSortDownloads,
		FilterPlatform: types.PlatformFabric,
	}

	u := searchUrl("fabric-api", options)

	// Should contain key parameters
	mustContain := []string{
		"gameId=432",
		"classId=6",
		"searchFilter=fabric-api",
		"sortField=6",
		"sortOrder=desc",
		"pageSize=50",
		"modLoaderType=4",
	}
	for _, param := range mustContain {
		if !containsSubstring(u, param) {
			t.Errorf("searchUrl missing param '%s' in URL: %s", param, u)
		}
	}
}

func TestSearchUrl_NoLoaderForPlatformAny(t *testing.T) {
	options := types.SearchOptions{
		SortBy:         types.SearchSortRelevance,
		FilterPlatform: types.PlatformAny,
	}

	u := searchUrl("test", options)

	if containsSubstring(u, "modLoaderType") {
		t.Errorf("searchUrl should not include modLoaderType for PlatformAny: %s", u)
	}
}

func TestSlugSearchUrl_ContainsSlug(t *testing.T) {
	u := slugSearchUrl("jei")

	mustContain := []string{
		"gameId=432",
		"classId=6",
		"slug=jei",
		"pageSize=50",
	}
	for _, param := range mustContain {
		if !containsSubstring(u, param) {
			t.Errorf("slugSearchUrl missing param '%s' in URL: %s", param, u)
		}
	}
}

func TestModDescriptionUrl_Stripped(t *testing.T) {
	u := modDescriptionUrl(12345, true)

	mustContain := []string{
		"/v1/mods/12345/description",
		"stripped=true",
	}
	for _, param := range mustContain {
		if !containsSubstring(u, param) {
			t.Errorf("modDescriptionUrl missing '%s' in URL: %s", param, u)
		}
	}
}

func TestModFilesUrl_WithFilters(t *testing.T) {
	u := modFilesUrl(12345, "1.20.1", 4)

	mustContain := []string{
		"/v1/mods/12345/files",
		"gameVersion=1.20.1",
		"modLoaderType=4",
		"pageSize=50",
	}
	for _, param := range mustContain {
		if !containsSubstring(u, param) {
			t.Errorf("modFilesUrl missing '%s' in URL: %s", param, u)
		}
	}
}

func TestModFilesUrl_NoFilters(t *testing.T) {
	u := modFilesUrl(999, "", 0)

	if containsSubstring(u, "gameVersion") {
		t.Errorf("modFilesUrl should not include gameVersion when empty: %s", u)
	}
	if containsSubstring(u, "modLoaderType") {
		t.Errorf("modFilesUrl should not include modLoaderType when 0: %s", u)
	}
}

// containsSubstring is a simple test helper.
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
