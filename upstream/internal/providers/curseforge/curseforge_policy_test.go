package curseforge

import (
	"testing"

	"github.com/mclucy/lucy/version"
)

func TestSelectLatestFile(t *testing.T) {
	files := []fileResponse{
		{
			ID:          1,
			IsAvailable: true,
			ChannelCode: 1,
			FileDate:    "2024-01-01T00:00:00Z",
		},
		{
			ID:          2,
			IsAvailable: true,
			ChannelCode: 2,
			FileDate:    "2024-02-01T00:00:00Z",
		},
	}

	tests := []struct {
		selector version.Version
		want     int32
	}{
		{version.Stable, 1},
		{version.Any, 1},
		{version.Beta, 2},
	}
	for _, tt := range tests {
		t.Run(tt.selector.String(), func(t *testing.T) {
			got := selectLatestFile(files, tt.selector)
			if got == nil {
				t.Fatalf("selectLatestFile(%s) returned nil", tt.selector)
			}
			if got.ID != tt.want {
				t.Fatalf(
					"selectLatestFile(%s) = %d, want %d",
					tt.selector,
					got.ID,
					tt.want,
				)
			}
		})
	}
}

func TestSelectLatestFile_StableRejectsPrereleaseOnly(t *testing.T) {
	files := []fileResponse{{
		ID:          2,
		IsAvailable: true,
		ChannelCode: 2,
		FileDate:    "2024-02-01T00:00:00Z",
	}}
	if got := selectLatestFile(files, version.Stable); got != nil {
		t.Fatalf("stable selector selected a prerelease: %v", got)
	}
}
