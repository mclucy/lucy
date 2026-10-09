package cli

import (
	"context"
	"os"
	"strings"

	"github.com/mclucy/lucy/lockfile"
	"github.com/mclucy/lucy/types"

	"github.com/spf13/cobra"
)

type CompletionCandidate struct {
	Value       string
	Description string
}

// FilterByPrefix keeps candidates whose Value starts with prefix,
// matching case-insensitively.
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

// ToCobraCompletions formats candidates as cobra's "value\tDescription" lines.
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

// StaticEcosystemCandidates covers all user-facing platforms.
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

// StaticSearchEcosystemCandidates is the search rollout subset, not the full
// platform set.
func StaticSearchEcosystemCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{Value: types.EcoFabric.String(), Description: "Fabric mods"},
		{Value: types.EcoForge.String(), Description: "Forge mods"},
		{Value: types.EcoNeoforge.String(), Description: "NeoForge mods"},
		{Value: "bukkit", Description: "Bukkit/Paper/Spigot plugins"},
	}
}

func StaticSearchSourceCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{Value: types.SourceModrinth.String(), Description: "Modrinth"},
		{Value: types.SourceCurseForge.String(), Description: "CurseForge"},
		{Value: types.SourceHangar.String(), Description: "Hangar"},
		{Value: types.SourceSpiget.String(), Description: "Spiget"},
		{Value: types.SourceMCDR.String(), Description: "MCDR Plugin Catalogue"},
	}
}

func StaticVersionCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{Value: "any", Description: "Latest version, any stability (default)"},
		{Value: "stable", Description: "Latest stable release, no betas"},
		{Value: "beta", Description: "Latest version, allow pre-releases"},
	}
}

// LockCandidates lists the package references the current lock resolves, with
// their versions. It is the only source of workspace package suggestions;
// nothing is inferred from outside the lock.
func LockCandidates(root string) []CompletionCandidate {
	lock, err := lockfile.Read(root)
	if err != nil {
		return nil
	}
	out := make([]CompletionCandidate, 0, len(lock.Packages))
	for _, pkg := range lock.Packages {
		reference := pkg.Provider + ":" + pkg.ProjectID
		description := pkg.Runtime + "/" + pkg.Loader.String() + " " + pkg.Version
		out = append(out, CompletionCandidate{
			Value:       reference,
			Description: description,
		})
	}
	return out
}

// ResolveLockReference resolves a native module name to the single manifest
// reference the current lock binds it to. An empty result means the lock
// resolves no such package; an error means the name is ambiguous and the
// caller must supply an explicit "provider:project".
func ResolveLockReference(root, runtime string, loader types.Ecosystem, name string) (string, error) {
	lock, err := lockfile.Read(root)
	if err != nil {
		return "", nil
	}
	return lock.ContextReference(runtime, loader, name)
}

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

// CompletePackageIDSuggestions completes a "provider:project@version" token
// for the current workspace. Workspace suggestions come from that
// workspace's lock, which is the only evidence of what it contains.
func CompletePackageIDSuggestions(
	ctx context.Context,
	commandName string,
	token string,
) ([]string, cobra.ShellCompDirective) {
	root, err := os.Getwd()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	return CompletePackageIDSuggestionsIn(ctx, root, commandName, token)
}

// CompletePackageIDSuggestionsIn completes a token against a specific
// workspace root.
func CompletePackageIDSuggestionsIn(
	ctx context.Context,
	root string,
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

	candidates := collectPackageIDSuggestionCandidates(ctx, root, request)
	return ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
}

func collectPackageIDSuggestionCandidates(
	ctx context.Context,
	root string,
	request PackageIDSuggestionContext,
) []CompletionCandidate {
	out := make([]CompletionCandidate, 0)
	if request.Segment == "name" {
		out = append(out, FilterByPrefix(LockCandidates(root), request.Name)...)
	}
	for _, provider := range packageIDSuggestionProviders {
		candidates, err := provider.SuggestPackageIDs(ctx, request)
		if err != nil {
			continue
		}
		out = append(out, candidates...)
	}
	return out
}

// ParseCompletionToken parses a partial "provider:project@version" completion
// token. Target ecosystem is selected by --platform, not package syntax.
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
