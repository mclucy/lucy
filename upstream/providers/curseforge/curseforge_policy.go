package curseforge

import (
	"github.com/mclucy/lucy/upstream"

	// selectLatestFile applies issue 60 selector semantics. stable accepts
	// releases only; beta accepts any channel; any prefers a release and falls
	// back to a prerelease.
	versionpkg "github.com/mclucy/lucy/version"
)

func selectLatestFile(
	files []fileResponse,
	selector versionpkg.Version,
) *fileResponse {
	switch selector {
	case versionpkg.Stable:
		return selectLatestReleaseFile(files)
	case versionpkg.Beta:
		return selectLatestAnyFile(files)
	default:
		if latest := selectLatestReleaseFile(files); latest != nil {
			return latest
		}
		return selectLatestAnyFile(files)
	}
}

func selectLatestAnyFile(files []fileResponse) *fileResponse {
	var latest *fileResponse
	for i := range files {
		candidate := &files[i]
		if !candidate.IsAvailable {
			continue
		}
		if latest == nil || candidate.FileDate > latest.FileDate {
			latest = candidate
		}
	}
	return latest
}

func selectLatestReleaseFile(files []fileResponse) *fileResponse {
	var latest *fileResponse
	for i := range files {
		candidate := &files[i]
		if !candidate.IsAvailable || candidate.ChannelCode != 1 {
			continue
		}
		if latest == nil || candidate.FileDate > latest.FileDate {
			latest = candidate
		}
	}
	return latest
}

// curseForgeChannel maps the CurseForge releaseType enum (1=Release,
// 2=Beta, 3=Alpha) to a typed release channel.
func curseForgeChannel(releaseType int32) upstream.ReleaseChannel {
	switch releaseType {
	case 1:
		return upstream.ReleaseChannelRelease
	case 2:
		return upstream.ReleaseChannelBeta
	case 3:
		return upstream.ReleaseChannelAlpha
	default:
		return upstream.ReleaseChannelUnknown
	}
}

func selectFileByVersion(files []fileResponse, version string) *fileResponse {
	for i := range files {
		candidate := &files[i]
		if !candidate.IsAvailable {
			continue
		}
		if candidate.DisplayName == version || candidate.FileName == version {
			return candidate
		}
	}
	return nil
}
