package modrinth

import (
	"testing"
	"time"

	"github.com/mclucy/lucy/types"
)

func TestSelectLatestVersionForSelector(t *testing.T) {
	release := &versionResponse{
		VersionNumber: "1.0.0",
		VersionType:   "release",
		DatePublished: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	beta := &versionResponse{
		VersionNumber: "1.1.0-beta",
		VersionType:   "beta",
		DatePublished: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
	}
	versions := []*versionResponse{release, beta}

	tests := []struct {
		selector types.Version
		want     *versionResponse
	}{
		{types.VersionStable, release},
		{types.VersionAny, release},
		{types.VersionBeta, beta},
	}
	for _, tt := range tests {
		t.Run(tt.selector.String(), func(t *testing.T) {
			got := selectLatestVersionForSelector(
				versions,
				types.EcosystemUnspecified,
				types.VersionUnknown,
				false,
				tt.selector,
			)
			if got != tt.want {
				t.Fatalf(
					"selectLatestVersionForSelector(%s) = %v, want %v",
					tt.selector,
					got,
					tt.want,
				)
			}
		})
	}
}

func TestSelectLatestVersionForSelector_StableRejectsPrereleaseOnly(t *testing.T) {
	beta := &versionResponse{
		VersionNumber: "1.1.0-beta",
		VersionType:   "beta",
		DatePublished: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
	}
	got := selectLatestVersionForSelector(
		[]*versionResponse{beta},
		types.EcosystemUnspecified,
		types.VersionUnknown,
		false,
		types.VersionStable,
	)
	if got != nil {
		t.Fatalf("stable selector selected a prerelease: %v", got)
	}
}
