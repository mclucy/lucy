package cli

import (
	"context"
	"strings"

	"github.com/mclucy/lucy/catalog"
	"github.com/mclucy/lucy/document"
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

// StaticEcosystemCandidates covers all user-facing ecosystems.
func StaticEcosystemCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{
			Value:       catalog.EcosystemMinecraft.String(),
			Description: "Vanilla / Bukkit / Paper plugins",
		},
		{Value: catalog.EcosystemFabric.String(), Description: "Fabric mods"},
		{Value: catalog.EcosystemForge.String(), Description: "Forge mods"},
		{Value: catalog.EcosystemNeoForge.String(), Description: "NeoForge mods"},
		{
			Value:       catalog.EcosystemMCDR.String(),
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
// ecosystem set.
func StaticSearchEcosystemCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{Value: catalog.EcosystemFabric.String(), Description: "Fabric mods"},
		{Value: catalog.EcosystemForge.String(), Description: "Forge mods"},
		{Value: catalog.EcosystemNeoForge.String(), Description: "NeoForge mods"},
		{Value: catalog.EcosystemBukkit.String(), Description: "Bukkit/Spigot plugins"},
		{Value: catalog.EcosystemPaper.String(), Description: "Paper plugins"},
	}
}

func StaticSearchRegistryCandidates() []CompletionCandidate {
	return []CompletionCandidate{
		{Value: catalog.RegistryModrinth.String(), Description: "Modrinth"},
		{Value: catalog.RegistryCurseForge.String(), Description: "CurseForge"},
		{Value: catalog.RegistryHangar.String(), Description: "Hangar"},
		{Value: catalog.RegistrySpiget.String(), Description: "Spiget"},
		{Value: catalog.RegistryMCDR.String(), Description: "MCDR Plugin Catalogue"},
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
	lock, err := document.LockFile(root).Read()
	if err != nil {
		return nil
	}
	out := make([]CompletionCandidate, 0, len(lock.Packages))
	for _, pkg := range lock.Packages {
		reference := pkg.Provider.String() + ":" + pkg.ProjectID
		description := string(pkg.Environment) + "/" + pkg.Ecosystem.String() + " " + pkg.Version.String()
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
func ResolveLockReference(root, name string) (string, error) {
	lock, err := document.LockFile(root).Read()
	if err != nil {
		return "", nil
	}
	return lock.LookupReference(name)
}

// CompletePackageIDSuggestions completes a "provider:project@version" token
// for the invocation workspace. Workspace suggestions come from that
// workspace's lock, which is the only evidence of what it contains.
func CompletePackageIDSuggestions(
	_ context.Context,
	root string,
	token string,
) ([]string, cobra.ShellCompDirective) {
	_, name, version, segment := ParseCompletionToken(token)
	switch segment {
	case "version":
		candidates := FilterByPrefix(StaticVersionCandidates(), version)
		return ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
	case "name":
		candidates := FilterByPrefix(LockCandidates(root), name)
		return ToCobraCompletions(candidates), cobra.ShellCompDirectiveNoFileComp
	default:
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
}

// ParseCompletionToken parses a partial "provider:project@version" completion
// token. Target ecosystem is selected by --ecosystem, not package syntax.
func ParseCompletionToken(token string) (provider, name, version, segment string) {
	provider = "auto"
	beforeVersion := token
	if before, after, ok := strings.Cut(token, "@"); ok {
		beforeVersion = before
		version = after
		segment = "version"
	}
	if before, after, ok := strings.Cut(beforeVersion, ":"); ok {
		provider = before
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
