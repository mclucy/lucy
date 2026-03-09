package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/mclucy/lucy/types"
	"github.com/urfave/cli/v3"
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
		{Value: types.PlatformMinecraft.String(), Description: ""},
		{Value: types.PlatformFabric.String(), Description: ""},
		{Value: types.PlatformForge.String(), Description: ""},
		{Value: types.PlatformNeoforge.String(), Description: ""},
		{Value: types.PlatformMCDR.String(), Description: ""},
	}
}

// StaticSourceCandidates returns completion candidates for concrete upstream sources.
func StaticSourceCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{Value: "curseforge", Description: ""},
		{Value: types.SourceModrinth.String(), Description: ""},
		{Value: types.SourceGitHub.String(), Description: ""},
		{Value: types.SourceMCDR.String(), Description: ""},
	}
}

// StaticSortCandidates returns completion candidates for search sort options.
func StaticSortCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{Value: string(types.SearchSortRelevance), Description: "Sort by relevance"},
		{Value: string(types.SearchSortDownloads), Description: "Sort by download count"},
		{Value: string(types.SearchSortNewest), Description: "Sort by newest"},
	}
}

// ParseCompletionToken parses a partial "platform/name@version" token for shell completion.
// Returns parsed components and the active segment ("platform", "name", or "version").
//
// Uses manual string splitting instead of syntax.Parse which panics on partial input.
func ParseCompletionToken(token string) (platform, name, version, segment string) {
	if before, after, ok := strings.Cut(token, "@"); ok {
		version = after
		beforeAt := before
		if slashIdx := strings.Index(beforeAt, "/"); slashIdx >= 0 {
			platform = beforeAt[:slashIdx]
			name = beforeAt[slashIdx+1:]
		} else {
			name = beforeAt
		}
		segment = "version"
		return
	}

	if before, after, ok := strings.Cut(token, "/"); ok {
		platform = before
		name = after
		segment = "name"
		return
	}

	platform = token
	segment = "platform"
	return
}

func currentCompletionToken() string {
	for i := len(os.Args) - 1; i >= 0; i-- {
		if os.Args[i] != "--generate-shell-completion" {
			return os.Args[i]
		}
	}
	return ""
}

func CompleteFlagNamesIfRequested(cmd *cli.Command) bool {
	token := currentCompletionToken()
	if !strings.HasPrefix(token, "-") {
		return false
	}

	PrintCandidates(FilterByPrefix(flagCompletionCandidates(cmd), token))
	return true
}

func flagCompletionCandidates(cmd *cli.Command) []CompletionCandidate {
	flags := append([]cli.Flag{}, cmd.VisibleFlags()...)
	flags = append(flags, cmd.VisiblePersistentFlags()...)

	seen := make(map[string]struct{})
	out := make([]CompletionCandidate, 0, len(flags)*2)

	for _, flag := range flags {
		usage := ""
		if usageProvider, ok := flag.(interface{ GetUsage() string }); ok {
			usage = usageProvider.GetUsage()
		}

		for _, name := range flag.Names() {
			if name == "" {
				continue
			}
			prefix := "--"
			if len(name) == 1 {
				prefix = "-"
			}

			value := prefix + name
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}

			out = append(out, CompletionCandidate{Value: value, Description: usage})
		}
	}

	return out
}
