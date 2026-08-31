package cli

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

func StaticStatusLogoCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{Value: "small", Description: "Compact logo (default)"},
		{Value: "none", Description: "Hide the logo"},
		{Value: "large", Description: "Full-size logo"},
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

// StaticSearchSourceCandidates returns candidates for search sources.
func StaticSearchSourceCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{Value: types.SourceModrinth.String(), Description: "Modrinth"},
		{Value: types.SourceCurseForge.String(), Description: "CurseForge"},
		{Value: types.SourceHangar.String(), Description: "Hangar"},
		{Value: types.SourceSpiget.String(), Description: "Spiget"},
		{Value: types.SourceMCDR.String(), Description: "MCDR Plugin Catalogue"},
	}
}

// StaticVersionCandidates returns completion candidates for fuzzy version hints.
func StaticVersionCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{Value: "any", Description: "Latest version, any stability (default)"},
		{Value: "stable", Description: "Latest stable release, no betas"},
		{Value: "beta", Description: "Latest version, allow pre-releases"},
	}
}

// ParseCompletionToken parses a partial "source:name@version" token for
// shell completion. It returns source, name, version, and the active segment.
// Target ecosystem is selected by --platform, not package syntax.
func ParseCompletionToken(token string) (source, name, version, segment string) {
	source = "auto"
	beforeVersion := token
	if before, after, ok := strings.Cut(token, "@"); ok {
		beforeVersion = before
		version = after
		segment = "version"
	}
	if before, after, ok := strings.Cut(beforeVersion, ":"); ok {
		source = before
		name = after
		if segment == "" {
			segment = "name"
		}
		return
	}
	name = beforeVersion
	if segment == "" {
		segment = "name"
	}
	return
}
