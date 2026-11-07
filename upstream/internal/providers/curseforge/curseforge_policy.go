package curseforge

import "github.com/mclucy/lucy/upstream"

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
