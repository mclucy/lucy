package types

import "strings"

// ReleaseChannel is the stability channel published by a provider.
type ReleaseChannel string

const (
	ReleaseChannelUnknown ReleaseChannel = ""
	ReleaseChannelRelease ReleaseChannel = "release"
	ReleaseChannelBeta    ReleaseChannel = "beta"
	ReleaseChannelAlpha   ReleaseChannel = "alpha"
)

func ParseReleaseChannel(s string) ReleaseChannel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "release", "stable":
		return ReleaseChannelRelease
	case "beta", "prerelease", "pre-release":
		return ReleaseChannelBeta
	case "alpha":
		return ReleaseChannelAlpha
	default:
		return ReleaseChannelUnknown
	}
}
