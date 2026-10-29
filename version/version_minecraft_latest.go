package version

import (
	"errors"
	"strconv"
	"strings"
)

// NewestMinecraft reports the highest version in the list under the Minecraft
// grammar. Entries the grammar cannot parse are skipped.
func NewestMinecraft(versions []Version) (Version, error) {
	var newest Version
	var parsed ResolvableVersion
	for _, candidate := range versions {
		if candidate == "" {
			continue
		}
		resolved, err := ParseVersion(candidate, DialectMinecraft)
		if err != nil || resolved == nil {
			continue
		}
		if parsed == nil {
			newest, parsed = candidate, resolved
			continue
		}
		if newerMinecraft(candidate, resolved, newest, parsed) {
			newest, parsed = candidate, resolved
		}
	}
	if parsed == nil {
		return "", errors.New("no Minecraft version in the candidate list")
	}
	return newest, nil
}

// newerMinecraft reports whether candidate is newer than current. The release
// scheme changed at year 26, and versions from different schemes do not
// compare, so a year-based version outranks every 1.x version.
func newerMinecraft(
	candidate Version,
	candidateParsed ResolvableVersion,
	current Version,
	currentParsed ResolvableVersion,
) bool {
	candidateScheme := minecraftSchemeRank(candidate)
	currentScheme := minecraftSchemeRank(current)
	if candidateScheme != currentScheme {
		return candidateScheme > currentScheme
	}
	comparison, comparable := Compare(currentParsed, candidateParsed)
	return comparable && comparison < 0
}

// minecraftSchemeRank orders the release schemes: 1.x releases rank 0 and
// year-based releases rank 1.
func minecraftSchemeRank(v Version) int {
	head, _, _ := strings.Cut(string(v), ".")
	year, err := strconv.Atoi(head)
	if err != nil {
		return 0
	}
	if year >= minPost26ReleaseYear {
		return 1
	}
	return 0
}
