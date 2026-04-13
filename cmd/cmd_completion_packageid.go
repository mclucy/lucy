package cmd

import (
	"context"
	"sort"

	"github.com/spf13/cobra"
)

type PackageIDSuggestionContext struct {
	Command  string
	Token    string
	Platform string
	Name     string
	Version  string
	Segment  string
}

type PackageIDSuggestionProvider interface {
	Name() string
	Priority() int
	SuggestPackageIDs(context.Context, PackageIDSuggestionContext) ([]CompletionCandidate, error)
}

var packageIDSuggestionProviders []PackageIDSuggestionProvider

func RegisterPackageIDSuggestionProvider(provider PackageIDSuggestionProvider) {
	if provider == nil {
		return
	}

	packageIDSuggestionProviders = append(packageIDSuggestionProviders, provider)
	sort.SliceStable(packageIDSuggestionProviders, func(i, j int) bool {
		return packageIDSuggestionProviders[i].Priority() < packageIDSuggestionProviders[j].Priority()
	})
}

func CompletePackageIDSuggestions(ctx context.Context, commandName string, token string) ([]string, cobra.ShellCompDirective) {
	platform, name, version, segment := ParseCompletionToken(token)

	if segment == "" || segment == "platform" {
		candidates := FilterByPrefix(StaticPlatformCandidates(), token)
		return ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
	}

	request := PackageIDSuggestionContext{
		Command:  commandName,
		Token:    token,
		Platform: platform,
		Name:     name,
		Version:  version,
		Segment:  segment,
	}

	candidates := collectPackageIDSuggestionCandidates(ctx, request)
	return ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
}

func collectPackageIDSuggestionCandidates(
	ctx context.Context,
	request PackageIDSuggestionContext,
) []CompletionCandidate {
	out := make([]CompletionCandidate, 0)
	for _, provider := range packageIDSuggestionProviders {
		candidates, err := provider.SuggestPackageIDs(ctx, request)
		if err != nil {
			continue
		}
		out = append(out, candidates...)
	}
	return out
}
