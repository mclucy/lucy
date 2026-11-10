package modrinth

import (
	"errors"
	"testing"
	"time"
)

func TestSelectImportVersion_MatchesNumberOrID(t *testing.T) {
	versions := []*versionResponse{
		{ID: "ver-2", VersionNumber: "2.0.0", VersionType: "release"},
		{ID: "ver-1", VersionNumber: "1.0.0", VersionType: "release"},
	}
	tests := []struct {
		selector string
		wantID   string
	}{
		{selector: "1.0.0", wantID: "ver-1"},
		{selector: "ver-2", wantID: "ver-2"},
	}
	for _, tt := range tests {
		got, err := selectImportVersion(versions, tt.selector)
		if err != nil {
			t.Fatalf("selector %q: unexpected error: %v", tt.selector, err)
		}
		if got.ID != tt.wantID {
			t.Errorf("selector %q: got %q, want %q", tt.selector, got.ID, tt.wantID)
		}
	}
}

func TestSelectImportVersion_UnknownSelector(t *testing.T) {
	versions := []*versionResponse{{ID: "ver-1", VersionNumber: "1.0.0"}}
	_, err := selectImportVersion(versions, "9.9.9")
	if !errors.Is(err, ErrNoVersion) {
		t.Fatalf("expected ErrNoVersion, got %v", err)
	}
}

func TestSelectImportVersion_EmptyPicksNewestRelease(t *testing.T) {
	// The newest version overall is a beta, and the list is not sorted by
	// date, so the result depends on the date and channel rather than order.
	versions := []*versionResponse{
		{ID: "old-release", VersionType: "release", DatePublished: day(2021)},
		{ID: "new-beta", VersionType: "beta", DatePublished: day(2023)},
		{ID: "new-release", VersionType: "release", DatePublished: day(2022)},
	}
	got, err := selectImportVersion(versions, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "new-release" {
		t.Errorf("got %q, want new-release", got.ID)
	}
}

func TestSelectImportVersion_EmptyFallsBackToNewestAnyType(t *testing.T) {
	versions := []*versionResponse{
		{ID: "alpha-old", VersionType: "alpha", DatePublished: day(2021)},
		{ID: "beta-new", VersionType: "beta", DatePublished: day(2023)},
	}
	got, err := selectImportVersion(versions, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.ID != "beta-new" {
		t.Errorf("got %q, want beta-new", got.ID)
	}
}

func TestSelectImportVersion_NoVersions(t *testing.T) {
	_, err := selectImportVersion(nil, "")
	if !errors.Is(err, ErrNoVersion) {
		t.Fatalf("expected ErrNoVersion, got %v", err)
	}
}

func TestMrpackFile_PrefersPrimary(t *testing.T) {
	files := []fileResponse{
		{Filename: "first.mrpack", URL: "https://example.test/first"},
		{Filename: "primary.mrpack", URL: "https://example.test/primary", IsPrimary: true},
	}
	got, ok := mrpackFile(files)
	if !ok {
		t.Fatal("expected an mrpack file")
	}
	if got.Filename != "primary.mrpack" {
		t.Errorf("got %q, want primary.mrpack", got.Filename)
	}
}

func TestMrpackFile_SkipsNonPackAndURLless(t *testing.T) {
	files := []fileResponse{
		{Filename: "mods.zip", URL: "https://example.test/zip", IsPrimary: true},
		{Filename: "pack.MRPACK", URL: ""},
		{Filename: "pack.MRPACK", URL: "https://example.test/pack"},
	}
	got, ok := mrpackFile(files)
	if !ok {
		t.Fatal("expected an mrpack file")
	}
	if got.URL != "https://example.test/pack" {
		t.Errorf("got URL %q, want the downloadable pack", got.URL)
	}
}

func TestMrpackFile_None(t *testing.T) {
	files := []fileResponse{{Filename: "mods.zip", URL: "https://example.test/zip"}}
	if _, ok := mrpackFile(files); ok {
		t.Error("expected no mrpack file")
	}
}

func TestFileHashes_KeepsPresentDigests(t *testing.T) {
	f := fileResponse{}
	f.Hashes.Sha1 = "sha1-digest"
	got := fileHashes(f)
	if got["sha1"] != "sha1-digest" {
		t.Errorf("expected sha1 digest, got %v", got)
	}
	if _, ok := got["sha512"]; ok {
		t.Errorf("expected no sha512 entry for an empty digest, got %v", got)
	}
}

func day(year int) time.Time {
	return time.Date(year, time.January, 1, 0, 0, 0, 0, time.UTC)
}
