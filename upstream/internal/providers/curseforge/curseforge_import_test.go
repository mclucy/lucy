package curseforge

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestNewestReleaseFile_PrefersReleaseOverNewerBeta(t *testing.T) {
	files := []fileResponse{
		{ID: 30, IsAvailable: true, ChannelCode: 2, FileDate: "2024-03-01T00:00:00Z"},
		{ID: 10, IsAvailable: true, ChannelCode: 1, FileDate: "2024-01-01T00:00:00Z"},
	}
	got := newestReleaseFile(files)
	if got == nil || got.ID != 10 {
		t.Fatalf("expected release file 10, got %+v", got)
	}
}

func TestNewestReleaseFile_FallsBackToNonReleaseWhenNoRelease(t *testing.T) {
	files := []fileResponse{
		{ID: 5, IsAvailable: true, ChannelCode: 3, FileDate: "2024-01-01T00:00:00Z"},
		{ID: 6, IsAvailable: true, ChannelCode: 2, FileDate: "2024-02-01T00:00:00Z"},
	}
	got := newestReleaseFile(files)
	if got == nil || got.ID != 6 {
		t.Fatalf("expected beta file 6, got %+v", got)
	}
}

func TestNewestReleaseFile_DateThenIDTieBreak(t *testing.T) {
	files := []fileResponse{
		{ID: 50, IsAvailable: true, ChannelCode: 1, FileDate: "2024-01-01T00:00:00Z"},
		{ID: 20, IsAvailable: true, ChannelCode: 1, FileDate: "2024-02-01T00:00:00Z"},
		{ID: 70, IsAvailable: true, ChannelCode: 1, FileDate: "2023-12-01T00:00:00Z"},
	}
	if got := newestReleaseFile(files); got == nil || got.ID != 20 {
		t.Fatalf("expected latest-dated file 20, got %+v", got)
	}

	sameDate := []fileResponse{
		{ID: 7, IsAvailable: true, ChannelCode: 1, FileDate: "2024-01-01T00:00:00Z"},
		{ID: 9, IsAvailable: true, ChannelCode: 1, FileDate: "2024-01-01T00:00:00Z"},
	}
	if got := newestReleaseFile(sameDate); got == nil || got.ID != 9 {
		t.Fatalf("expected higher ID 9 on date tie, got %+v", got)
	}
}

func TestNewestReleaseFile_SkipsUnavailable(t *testing.T) {
	files := []fileResponse{
		{ID: 99, IsAvailable: false, ChannelCode: 1},
	}
	if got := newestReleaseFile(files); got != nil {
		t.Fatalf("expected no file, got %+v", got)
	}
}

func TestServerPackFor_FileIsItsOwnServerPack(t *testing.T) {
	p := &Provider{}
	isServer := true
	file := &fileResponse{ID: 3, IsServerPack: &isServer}
	got, err := p.serverPackFor(context.Background(), 1, file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != file {
		t.Errorf("expected the file itself, got %+v", got)
	}
}

func TestServerPackFor_NoServerPackIsActionable(t *testing.T) {
	p := &Provider{}
	isServer := false
	file := &fileResponse{ID: 3, DisplayName: "1.0", IsServerPack: &isServer}
	_, err := p.serverPackFor(context.Background(), 1, file)
	if !errors.Is(err, ErrNoServerPack) {
		t.Fatalf("expected ErrNoServerPack, got %v", err)
	}
}

func TestSelectModpackFile_InvalidFileID(t *testing.T) {
	p := &Provider{}
	_, err := p.selectModpackFile(context.Background(), &modResponse{ID: 1}, "1.0.0")
	if !errors.Is(err, ErrInvalidFileID) {
		t.Fatalf("expected ErrInvalidFileID, got %v", err)
	}
}

func TestDownloadableURL(t *testing.T) {
	url := "https://example.test/pack.zip"
	empty := ""
	tests := []struct {
		name string
		file fileResponse
		ok   bool
	}{
		{name: "available with URL", file: fileResponse{IsAvailable: true, DownloadUrl: &url}, ok: true},
		{name: "nil URL", file: fileResponse{IsAvailable: true}},
		{name: "empty URL", file: fileResponse{IsAvailable: true, DownloadUrl: &empty}},
		{name: "unavailable", file: fileResponse{IsAvailable: false, DownloadUrl: &url}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := downloadableURL(&tt.file)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v", ok, tt.ok)
			}
			if ok && got != url {
				t.Errorf("got %q, want %q", got, url)
			}
		})
	}
}

func TestFileSHA1Hashes_KeepsOnlySHA1(t *testing.T) {
	got := fileSHA1Hashes([]fileHash{
		{Algo: 2, Digest: "md5-digest"},
		{Algo: 1, Digest: "sha1-digest"},
	})
	if got["sha1"] != "sha1-digest" || len(got) != 1 {
		t.Errorf("expected only sha1, got %v", got)
	}
}

func TestSlugSearchUrlForClass_UsesClassID(t *testing.T) {
	u := slugSearchUrlForClass("all-the-mods", modpacksClassId)
	if !strings.Contains(u, "classId=4471") {
		t.Errorf("expected modpack classId in URL: %s", u)
	}
	if !strings.Contains(u, "slug=all-the-mods") {
		t.Errorf("expected slug in URL: %s", u)
	}
}
