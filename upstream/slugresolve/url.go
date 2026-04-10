package slugresolve

import (
	"net/url"
	"strings"

	"github.com/mclucy/lucy/types"
)

// ExtractFromURL parses a mod homepage URL and returns the upstream source
// and canonical slug if the URL is a recognised Modrinth or CurseForge
// project page.
//
// Recognised patterns:
//
//	https://modrinth.com/mod/<slug>
//	https://modrinth.com/plugin/<slug>
//	https://modrinth.com/datapack/<slug>
//	https://www.curseforge.com/minecraft/mc-mods/<slug>
//	https://curseforge.com/minecraft/mc-mods/<slug>
func ExtractFromURL(rawURL string) (src types.Source, slug string, ok bool) {
	if rawURL == "" {
		return types.SourceAuto, "", false
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return types.SourceAuto, "", false
	}
	host := strings.TrimPrefix(u.Host, "www.")
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")

	switch host {
	case "modrinth.com":
		// /mod/<slug>, /plugin/<slug>, /datapack/<slug>
		if len(parts) >= 2 {
			return types.SourceModrinth, parts[1], true
		}
	case "curseforge.com":
		// /minecraft/mc-mods/<slug>
		if len(parts) >= 3 && parts[0] == "minecraft" && parts[1] == "mc-mods" {
			return types.SourceCurseForge, parts[2], true
		}
	}
	return types.SourceAuto, "", false
}
