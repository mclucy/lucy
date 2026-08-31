package cli

import (
	"context"

	"github.com/spf13/cobra"
)

type PackageIDSuggestionContext struct {
	Command string
	Token   string
	Source  string
	Name    string
	Version string
	Segment string
}

type PackageIDSuggestionProvider interface {
	Name() string
	Priority() int
	SuggestPackageIDs(
		context.Context,
		PackageIDSuggestionContext,
	) ([]CompletionCandidate, error)
}

var packageIDSuggestionProviders []PackageIDSuggestionProvider

func CompletePackageIDSuggestions(
	ctx context.Context,
	commandName string,
	token string,
) ([]string, cobra.ShellCompDirective) {
	source, name, version, segment := ParseCompletionToken(token)

	if segment == "version" {
		candidates := FilterByPrefix(StaticVersionCandidates(), version)
		return ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
	}

	request := PackageIDSuggestionContext{
		Command: commandName,
		Token:   token,
		Source:  source,
		Name:    name,
		Version: version,
		Segment: segment,
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
