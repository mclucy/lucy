package curseforge

import (
	"context"
	"strings"

	"github.com/mclucy/lucy/upstream"
)

// Builds lists every file published by a CurseForge project. project accepts
// either a numeric mod ID or a slug. The list is unfiltered: no game version,
// loader, channel, availability flag, or download URL state is excluded.
func (provider) Builds(
	ctx context.Context,
	project string,
) ([]upstream.Build, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	modID, err := resolveProjectID(project)
	if err != nil {
		return nil, err
	}

	files, err := listAllFiles(modID, "", 0)
	if err != nil {
		return nil, err
	}

	builds := make([]upstream.Build, len(files))
	for i := range files {
		gameVersions, loaders := splitGameVersions(files[i].GameVersions)
		builds[i] = upstream.Build{
			Version:      files[i].DisplayName,
			GameVersions: gameVersions,
			Loaders:      loaders,
			Channel:      string(curseForgeChannel(files[i].ChannelCode)),
			Published:    parseCurseforgeDate(files[i].FileDate),
		}
	}
	return builds, nil
}

// curseForgeLoaderMarkers holds the loader names CurseForge mixes into a
// file's gameVersions list. Keys are the lowercase loader identifiers used by
// the other providers.
var curseForgeLoaderMarkers = map[string]struct{}{
	"forge":      {},
	"cauldron":   {},
	"liteloader": {},
	"fabric":     {},
	"quilt":      {},
	"neoforge":   {},
	"modloader":  {},
	"rift":       {},
	"bukkit":     {},
	"spigot":     {},
	"paper":      {},
	"purpur":     {},
	"folia":      {},
	"sponge":     {},
	"velocity":   {},
	"bungeecord": {},
	"waterfall":  {},
	"travertine": {},
}

// splitGameVersions separates CurseForge's mixed gameVersions list into game
// versions and loader identifiers. Game versions start with a digit; loader
// markers are matched case-insensitively against the known loader names.
// Unrecognized markers are left out rather than being misreported as either.
func splitGameVersions(entries []string) (
	gameVersions []string,
	loaders []string,
) {
	for _, entry := range entries {
		trimmed := strings.TrimSpace(entry)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if _, ok := curseForgeLoaderMarkers[lower]; ok {
			loaders = append(loaders, lower)
			continue
		}
		if isGameVersion(trimmed) {
			gameVersions = append(gameVersions, trimmed)
		}
	}
	return gameVersions, loaders
}

func isGameVersion(s string) bool {
	if s == "" {
		return false
	}
	c := s[0]
	return c >= '0' && c <= '9'
}
