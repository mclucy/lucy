package routing

import (
	"context"
	"fmt"
	"strings"

	"github.com/mclucy/lucy/version"
)

type purpurVersionInfo struct {
	Builds struct {
		Latest string   `json:"latest"`
		All    []string `json:"all"`
	} `json:"builds"`
}

type purpurBuildInfo struct {
	Build   string `json:"build"`
	Version string `json:"version"`
	MD5     string `json:"md5"`
}

// IdentifyPurpurBuild matches a Purpur jar digest against the published builds
// for a Minecraft version. The Purpur API exposes only MD5 digests, so init
// walks builds from newest to oldest until one matches.
func (r *Router) IdentifyPurpurBuild(
	ctx context.Context,
	minecraft version.Version,
	md5 string,
) (version.Version, bool, error) {
	base := "https://api.purpurmc.org/v2/purpur/" + minecraft.String()
	var listed purpurVersionInfo
	if err := r.Metadata(ctx, base, &listed); err != nil {
		return "", false, err
	}
	builds := listed.Builds.All
	if len(builds) == 0 && listed.Builds.Latest != "" {
		builds = []string{listed.Builds.Latest}
	}
	for index := len(builds) - 1; index >= 0; index-- {
		build := builds[index]
		var detail purpurBuildInfo
		if err := r.Metadata(ctx, base+"/"+build, &detail); err != nil {
			return "", false, fmt.Errorf("Purpur build %s: %w", build, err)
		}
		if detail.Version != "" && detail.Version != minecraft.String() {
			continue
		}
		if strings.EqualFold(detail.MD5, md5) {
			if detail.Build != "" {
				build = detail.Build
			}
			return version.Version(build), true, nil
		}
	}
	return "", false, nil
}
