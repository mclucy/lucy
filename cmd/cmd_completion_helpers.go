package cmd

import (
	"strings"

	"github.com/mclucy/lucy/types"
)

// CompletionCandidate holds a value and optional description for shell completion.
type CompletionCandidate struct {
	Value       string
	Description string
}

// FilterByPrefix returns candidates whose Value starts with prefix (case-insensitive).
func FilterByPrefix(
	candidates []CompletionCandidate,
	prefix string,
) []CompletionCandidate {
	if prefix == "" {
		return candidates
	}
	lower := strings.ToLower(prefix)
	var out []CompletionCandidate
	for _, c := range candidates {
		if strings.HasPrefix(strings.ToLower(c.Value), lower) {
			out = append(out, c)
		}
	}
	return out
}

// ToCobraCompletions converts CompletionCandidate slice to cobra's "value\tDescription" format.
func ToCobraCompletions(candidates []CompletionCandidate) []string {
	out := make([]string, 0, len(candidates))
	for _, c := range candidates {
		if c.Description != "" {
			out = append(out, c.Value+"\t"+c.Description)
		} else {
			out = append(out, c.Value)
		}
	}
	return out
}

// StaticEcosystemCandidates returns completion candidates for all user-facing platforms.
func StaticEcosystemCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{
			Value:       types.EcoMinecraft.String(),
			Description: "Vanilla / Bukkit / Paper plugins",
		},
		{Value: types.EcoFabric.String(), Description: "Fabric mods"},
		{Value: types.EcoForge.String(), Description: "Forge mods"},
		{Value: types.EcoNeoforge.String(), Description: "NeoForge mods"},
		{
			Value:       types.EcoMcdr.String(),
			Description: "MCDR controller / plugin framework",
		},
	}
}

// StaticSearchEcosystemCandidates returns completion candidates for search-enabled platforms (rollout set).
func StaticSearchEcosystemCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{Value: types.EcoFabric.String(), Description: "Fabric mods"},
		{Value: types.EcoForge.String(), Description: "Forge mods"},
		{Value: types.EcoNeoforge.String(), Description: "NeoForge mods"},
		{Value: "bukkit", Description: "Bukkit/Paper/Spigot plugins"},
	}
}

// StaticVersionCandidates returns completion candidates for fuzzy version hints.
func StaticVersionCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{
			Value:       types.VersionCompatible.String(),
			Description: "Newest version that appears to fit the environment",
		},
		{Value: "latest", Description: "Request the newest available version"},
	}
}

// StaticSourceCandidates returns completion candidates for concrete upstream sources.
func StaticSourceCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{Value: "curseforge", Description: "CurseForge source"},
		{Value: types.SourceModrinth.String(), Description: "Modrinth source"},
		{Value: types.SourceGitHub.String(), Description: "GitHub Releases"},
		{
			Value:       types.SourceMCDR.String(),
			Description: "MCDR Plugin Catalogue",
		},
	}
}

// StaticSortCandidates returns completion candidates for search sort options.
func StaticSortCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{Value: string(SearchSortRelevance), Description: "Sort by relevance"},
		{
			Value:       string(SearchSortDownloads),
			Description: "Sort by download count",
		},
		{Value: string(SearchSortNewest), Description: "Sort by newest"},
	}
}

// ParseCompletionToken parses a partial "platform/name@version" token for shell completion.
// Returns parsed components and the active segment ("platform", "name", or "version").
//
// Uses manual string splitting instead of syntax.Parse which panics on partial input.
func ParseCompletionToken(token string) (eco, name, version, segment string) {
	if before, after, ok := strings.Cut(token, "@"); ok {
		version = after
		if beforeSlash, afterSlash, hasSlash := strings.Cut(
			before,
			"/",
		); hasSlash {
			eco = beforeSlash
			name = afterSlash
		} else {
			name = before
		}
		segment = "version"
		return
	}

	if before, after, ok := strings.Cut(token, "/"); ok {
		eco = before
		name = after
		segment = "name"
		return
	}

	eco = token
	segment = "ecosystem"
	return
}
