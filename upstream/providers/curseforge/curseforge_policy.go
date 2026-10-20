package curseforge

import "github.com/mclucy/lucy/types"

// selectLatestFile applies issue 60 selector semantics. stable accepts
// releases only; beta accepts any channel; any prefers a release and falls
// back to a prerelease.
func selectLatestFile(
	files []fileResponse,
	selector types.Version,
) *fileResponse {
	switch selector {
	case types.VersionStable:
		return selectLatestReleaseFile(files)
	case types.VersionBeta:
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
func curseForgeChannel(releaseType int32) types.ReleaseChannel {
	switch releaseType {
	case 1:
		return types.ReleaseChannelRelease
	case 2:
		return types.ReleaseChannelBeta
	case 3:
		return types.ReleaseChannelAlpha
	default:
		return types.ReleaseChannelUnknown
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
