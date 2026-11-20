package routing

import (
	"context"
	"strings"

	"github.com/mclucy/lucy/version"
)

type purpurBuildInfo struct {
	Build   string `json:"build"`
	Version string `json:"version"`
	MD5     string `json:"md5"`
}

type purpurVersionInfo struct {
	Builds struct {
		Latest purpurBuildInfo   `json:"latest"`
		All    []purpurBuildInfo `json:"all"`
	} `json:"builds"`
}

// IdentifyPurpurBuild matches a Purpur jar digest against the published builds
// for a Minecraft version. The detailed version document carries every build's
// MD5 in one response, so init walks builds from newest to oldest until one
// matches. Failed builds omit the digest, and an empty digest never matches.
func (r *Router) IdentifyPurpurBuild(
	ctx context.Context,
	minecraft version.Version,
	md5 string,
) (version.Version, bool, error) {
	if md5 == "" {
		return "", false, nil
	}
	base := "https://api.purpurmc.org/v2/purpur/" + minecraft.String()
	var listed purpurVersionInfo
	if err := r.Metadata(ctx, base+"?detailed=true", &listed); err != nil {
		return "", false, err
	}
	builds := listed.Builds.All
	if len(builds) == 0 {
		builds = []purpurBuildInfo{listed.Builds.Latest}
	}
	for index := len(builds) - 1; index >= 0; index-- {
		build := builds[index]
		if build.Build == "" {
			continue
		}
		if build.Version != "" && build.Version != minecraft.String() {
			continue
		}
		if build.MD5 == "" {
			continue
		}
		if strings.EqualFold(build.MD5, md5) {
			return version.Version(build.Build), true, nil
		}
	}
	return "", false, nil
}
