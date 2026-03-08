package cmd

import (
	"fmt"
	"strings"
)

// CompletionCandidate holds a value and optional description for shell completion.
type CompletionCandidate struct {
	Value       string
	Description string
}

// PrintCandidates outputs candidates to stdout in urfave/cli v3 "value:description" format.
func PrintCandidates(candidates []CompletionCandidate) {
	for _, c := range candidates {
		if c.Description != "" {
			fmt.Printf("%s:%s\n", c.Value, c.Description)
		} else {
			fmt.Println(c.Value)
		}
	}
}

// FilterByPrefix returns candidates whose Value starts with prefix (case-insensitive).
func FilterByPrefix(candidates []CompletionCandidate, prefix string) []CompletionCandidate {
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

// StaticPlatformCandidates returns completion candidates for all user-facing platforms.
func StaticPlatformCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{Value: "minecraft", Description: "Vanilla Minecraft"},
		{Value: "fabric", Description: "Fabric mod loader"},
		{Value: "forge", Description: "Forge mod loader"},
		{Value: "neoforge", Description: "NeoForge mod loader"},
		{Value: "mcdr", Description: "MCDReforged plugin manager"},
	}
}

// StaticSourceCandidates returns completion candidates for concrete upstream sources.
func StaticSourceCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{Value: "curseforge", Description: "CurseForge"},
		{Value: "modrinth", Description: "Modrinth"},
		{Value: "github", Description: "GitHub"},
		{Value: "mcdr", Description: "MCDR"},
	}
}

// StaticSortCandidates returns completion candidates for search sort options.
func StaticSortCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{Value: "relevance", Description: "Sort by relevance"},
		{Value: "downloads", Description: "Sort by download count"},
		{Value: "newest", Description: "Sort by newest"},
	}
}

// ParseCompletionToken parses a partial "platform/name@version" token for shell completion.
// Returns parsed components and the active segment ("platform", "name", or "version").
//
// Uses manual string splitting instead of syntax.Parse which panics on partial input.
func ParseCompletionToken(token string) (platform, name, version, segment string) {
	if atIdx := strings.Index(token, "@"); atIdx >= 0 {
		version = token[atIdx+1:]
		beforeAt := token[:atIdx]
		if slashIdx := strings.Index(beforeAt, "/"); slashIdx >= 0 {
			platform = beforeAt[:slashIdx]
			name = beforeAt[slashIdx+1:]
		} else {
			name = beforeAt
		}
		segment = "version"
		return
	}

	if slashIdx := strings.Index(token, "/"); slashIdx >= 0 {
		platform = token[:slashIdx]
		name = token[slashIdx+1:]
		segment = "name"
		return
	}

	platform = token
	segment = "platform"
	return
}
